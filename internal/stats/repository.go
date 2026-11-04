// Отчёт одним запросом из четырёх независимых CTE, по одной на метрику, и в
// каждой стоит фильтр по team_id. Метрики нельзя считать одним JOIN задач с
// комментариями: он размножил бы строки и исказил и счётчики, и среднее.
//
// Определения: топ считается по закрытым задачам, у которых closed_at не раньше
// границы окна (граница включительно), по текущему исполнителю; при равенстве
// выигрывает меньший id, поэтому порядок детерминирован. Среднее время закрытия
// берётся по всем задачам команды с непустым closed_at, то есть по тем, что
// сейчас в статусе done. Команда без задач даёт нули и пустой список.

package stats

import (
	"context"
	"database/sql"
	"time"

	"github.com/vzinkovsky95/task-service-secunda/internal/db"
	"github.com/vzinkovsky95/task-service-secunda/internal/task"
)

const (
	metricStatus   = "status"
	metricTop      = "top_assignee"
	metricAvgClose = "avg_close_seconds"
	metricComments = "comments"
)

const reportQuery = `
WITH
status_counts AS (
    SELECT status, COUNT(*) AS task_count
    FROM tasks
    WHERE team_id = ?
    GROUP BY status
),
top_assignees AS (
    SELECT ROW_NUMBER() OVER (ORDER BY COUNT(*) DESC, t.assignee_id) AS ranking,
           t.assignee_id AS user_id,
           u.name AS user_name,
           COUNT(*) AS closed_count
    FROM tasks t
    JOIN users u ON u.id = t.assignee_id
    WHERE t.team_id = ? AND t.closed_at >= ?
    GROUP BY t.assignee_id, u.name
),
close_time AS (
    SELECT AVG(TIMESTAMPDIFF(MICROSECOND, created_at, closed_at)) / 1000000 AS avg_seconds
    FROM tasks
    WHERE team_id = ? AND closed_at IS NOT NULL
),
comment_totals AS (
    SELECT COUNT(*) AS comment_count
    FROM task_comments c
    JOIN tasks t ON t.id = c.task_id
    WHERE t.team_id = ?
)
SELECT '` + metricStatus + `' AS metric, 0 AS ranking, NULL AS user_id, CAST(status AS CHAR) AS label, task_count AS amount
FROM status_counts
UNION ALL
SELECT '` + metricTop + `', ranking, user_id, user_name, closed_count
FROM top_assignees
WHERE ranking <= 3
UNION ALL
SELECT '` + metricAvgClose + `', 0, NULL, NULL, avg_seconds
FROM close_time
UNION ALL
SELECT '` + metricComments + `', 0, NULL, NULL, comment_count
FROM comment_totals
ORDER BY metric, ranking`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (r *Repository) Report(ctx context.Context, q db.Querier, teamID uint64, now time.Time) (Report, error) {
	since := now.UTC().Add(-window)

	rows, err := q.QueryContext(ctx, reportQuery, teamID, teamID, since, teamID, teamID)
	if err != nil {
		return Report{}, err
	}
	defer rows.Close()

	report := Report{
		TeamID:        teamID,
		TasksByStatus: map[task.Status]int{},
		TopAssignees:  []Assignee{},
	}
	for _, status := range task.Statuses() {
		report.TasksByStatus[status] = 0
	}

	for rows.Next() {
		var metric string
		var ranking int
		var userID sql.NullInt64
		var label sql.NullString
		var amount sql.NullFloat64
		if err := rows.Scan(&metric, &ranking, &userID, &label, &amount); err != nil {
			return Report{}, err
		}

		switch metric {
		case metricStatus:
			report.TasksByStatus[task.Status(label.String)] = int(amount.Float64)
		case metricTop:
			report.TopAssignees = append(report.TopAssignees, Assignee{
				UserID:      uint64(userID.Int64),
				Name:        label.String,
				ClosedTasks: int(amount.Float64),
			})
		case metricAvgClose:
			report.AvgCloseSeconds = amount.Float64
		case metricComments:
			report.CommentsTotal = int(amount.Float64)
		}
	}
	return report, rows.Err()
}
