SET NAMES utf8mb4;
SET @password_hash = '$2a$10$ocbSf01QdiCVTVpHnFSytOs2Bs8hhqPiDaqkPSQNCZtdhbhZNpkLq';
SET @now = UTC_TIMESTAMP(6);

INSERT INTO users (email, password_hash, name, created_at) VALUES
    ('demo-owner@example.com', @password_hash, 'Анна Иванова', (@now - INTERVAL 60 DAY - INTERVAL 0 HOUR)),
    ('demo-admin@example.com', @password_hash, 'Борис Петров', (@now - INTERVAL 60 DAY - INTERVAL 0 HOUR)),
    ('demo-member@example.com', @password_hash, 'Вера Смирнова', (@now - INTERVAL 60 DAY - INTERVAL 0 HOUR)),
    ('demo-member2@example.com', @password_hash, 'Глеб Кузнецов', (@now - INTERVAL 60 DAY - INTERVAL 0 HOUR)),
    ('demo-owner-b@example.com', @password_hash, 'Дарья Орлова', (@now - INTERVAL 60 DAY - INTERVAL 0 HOUR)),
    ('demo-outsider@example.com', @password_hash, 'Егор Волков', (@now - INTERVAL 60 DAY - INTERVAL 0 HOUR));
SET @anna = (SELECT id FROM users WHERE email = 'demo-owner@example.com');
SET @boris = (SELECT id FROM users WHERE email = 'demo-admin@example.com');
SET @vera = (SELECT id FROM users WHERE email = 'demo-member@example.com');
SET @gleb = (SELECT id FROM users WHERE email = 'demo-member2@example.com');
SET @daria = (SELECT id FROM users WHERE email = 'demo-owner-b@example.com');
SET @egor = (SELECT id FROM users WHERE email = 'demo-outsider@example.com');

INSERT INTO teams (name, created_by, created_at) VALUES ('Разработка', @anna, (@now - INTERVAL 55 DAY - INTERVAL 0 HOUR));
SET @team_dev = LAST_INSERT_ID();
INSERT INTO teams (name, created_by, created_at) VALUES ('Маркетинг', @daria, (@now - INTERVAL 50 DAY - INTERVAL 0 HOUR));
SET @team_mkt = LAST_INSERT_ID();
INSERT INTO team_members (team_id, user_id, role) VALUES
    (@team_dev, @anna, 'owner'),
    (@team_dev, @boris, 'admin'),
    (@team_dev, @vera, 'member'),
    (@team_dev, @gleb, 'member'),
    (@team_mkt, @daria, 'owner'),
    (@team_mkt, @vera, 'admin');

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Подготовить релиз 1.0', 'Собрать changelog, проверить миграции и выкатить на стенд.', 'in_progress', @anna, @vera, (@now - INTERVAL 6 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 5 DAY - INTERVAL 2 HOUR), NULL, 2);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @anna, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Подготовить релиз 1.0'), 'description', JSON_OBJECT('old', NULL, 'new', 'Собрать changelog, проверить миграции и выкатить на стенд.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @vera)), (@now - INTERVAL 6 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 5 DAY - INTERVAL 2 HOUR));
INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES
    (@task, @vera, 'Начала, нужен доступ к стенду.', (@now - INTERVAL 5 DAY - INTERVAL 1 HOUR)),
    (@task, @anna, 'Доступ выдан, можно выкатывать.', (@now - INTERVAL 4 DAY - INTERVAL 20 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Обновить зависимости', 'Пройтись по go.mod и обновить минорные версии.', 'todo', @boris, NULL, (@now - INTERVAL 5 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 5 DAY - INTERVAL 0 HOUR), NULL, 1);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @boris, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Обновить зависимости'), 'description', JSON_OBJECT('old', NULL, 'new', 'Пройтись по go.mod и обновить минорные версии.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', NULL)), (@now - INTERVAL 5 DAY - INTERVAL 0 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Исправить ошибку авторизации', 'После смены пароля старый токен продолжает работать.', 'done', @anna, @vera, (@now - INTERVAL 20 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 3 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 3 DAY - INTERVAL 0 HOUR), 3);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @anna, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Исправить ошибку авторизации'), 'description', JSON_OBJECT('old', NULL, 'new', 'После смены пароля старый токен продолжает работать.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @vera)), (@now - INTERVAL 20 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 19 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'in_progress', 'new', 'done'), 'closed_at', JSON_OBJECT('old', NULL, 'new', DATE_FORMAT((@now - INTERVAL 3 DAY - INTERVAL 0 HOUR), '%Y-%m-%dT%H:%i:%s.%fZ'))), (@now - INTERVAL 3 DAY - INTERVAL 0 HOUR));
INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES
    (@task, @gleb, 'Проверил на стенде, ошибка не воспроизводится.', (@now - INTERVAL 3 DAY - INTERVAL 1 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Написать документацию API', 'Описать все эндпоинты и коды ошибок в OpenAPI.', 'done', @boris, @gleb, (@now - INTERVAL 12 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 2 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 2 DAY - INTERVAL 0 HOUR), 3);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @boris, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Написать документацию API'), 'description', JSON_OBJECT('old', NULL, 'new', 'Описать все эндпоинты и коды ошибок в OpenAPI.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @gleb)), (@now - INTERVAL 12 DAY - INTERVAL 0 HOUR)),
    (@task, @gleb, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 11 DAY - INTERVAL 0 HOUR)),
    (@task, @gleb, JSON_OBJECT('status', JSON_OBJECT('old', 'in_progress', 'new', 'done'), 'closed_at', JSON_OBJECT('old', NULL, 'new', DATE_FORMAT((@now - INTERVAL 2 DAY - INTERVAL 0 HOUR), '%Y-%m-%dT%H:%i:%s.%fZ'))), (@now - INTERVAL 2 DAY - INTERVAL 0 HOUR));
INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES
    (@task, @boris, 'Добавь примеры ответов.', (@now - INTERVAL 10 DAY - INTERVAL 0 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Настроить CI', 'Сборка, линтер и тесты на каждый пуш.', 'done', @anna, @boris, (@now - INTERVAL 40 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 35 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 35 DAY - INTERVAL 0 HOUR), 3);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @anna, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Настроить CI'), 'description', JSON_OBJECT('old', NULL, 'new', 'Сборка, линтер и тесты на каждый пуш.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @boris)), (@now - INTERVAL 40 DAY - INTERVAL 0 HOUR)),
    (@task, @boris, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 39 DAY - INTERVAL 0 HOUR)),
    (@task, @boris, JSON_OBJECT('status', JSON_OBJECT('old', 'in_progress', 'new', 'done'), 'closed_at', JSON_OBJECT('old', NULL, 'new', DATE_FORMAT((@now - INTERVAL 35 DAY - INTERVAL 0 HOUR), '%Y-%m-%dT%H:%i:%s.%fZ'))), (@now - INTERVAL 35 DAY - INTERVAL 0 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Провести ревью кода', 'Посмотреть изменения по задаче с кешем.', 'todo', @boris, @gleb, (@now - INTERVAL 2 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 2 DAY - INTERVAL 0 HOUR), NULL, 1);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @boris, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Провести ревью кода'), 'description', JSON_OBJECT('old', NULL, 'new', 'Посмотреть изменения по задаче с кешем.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @gleb)), (@now - INTERVAL 2 DAY - INTERVAL 0 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Оптимизировать запросы списка', 'Список задач команды медленный на больших данных.', 'in_progress', @vera, @vera, (@now - INTERVAL 4 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 3 DAY - INTERVAL 0 HOUR), NULL, 2);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @vera, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Оптимизировать запросы списка'), 'description', JSON_OBJECT('old', NULL, 'new', 'Список задач команды медленный на больших данных.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @vera)), (@now - INTERVAL 4 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 3 DAY - INTERVAL 0 HOUR));
INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES
    (@task, @anna, 'Сколько это займёт?', (@now - INTERVAL 3 DAY - INTERVAL 5 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Рефакторинг модуля уведомлений', 'Убрать дублирование в отправке писем.', 'done', @anna, @vera, (@now - INTERVAL 9 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 1 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 1 DAY - INTERVAL 0 HOUR), 3);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @anna, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Рефакторинг модуля уведомлений'), 'description', JSON_OBJECT('old', NULL, 'new', 'Убрать дублирование в отправке писем.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @vera)), (@now - INTERVAL 9 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 8 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'in_progress', 'new', 'done'), 'closed_at', JSON_OBJECT('old', NULL, 'new', DATE_FORMAT((@now - INTERVAL 1 DAY - INTERVAL 0 HOUR), '%Y-%m-%dT%H:%i:%s.%fZ'))), (@now - INTERVAL 1 DAY - INTERVAL 0 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Добавить метрики', 'Время ответа и число ошибок по маршрутам.', 'done', @boris, @vera, (@now - INTERVAL 15 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 7 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 7 DAY - INTERVAL 0 HOUR), 3);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @boris, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Добавить метрики'), 'description', JSON_OBJECT('old', NULL, 'new', 'Время ответа и число ошибок по маршрутам.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @vera)), (@now - INTERVAL 15 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 14 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'in_progress', 'new', 'done'), 'closed_at', JSON_OBJECT('old', NULL, 'new', DATE_FORMAT((@now - INTERVAL 7 DAY - INTERVAL 0 HOUR), '%Y-%m-%dT%H:%i:%s.%fZ'))), (@now - INTERVAL 7 DAY - INTERVAL 0 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Обновить README', 'Описать запуск и примеры запросов.', 'done', @anna, @gleb, (@now - INTERVAL 14 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 10 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 10 DAY - INTERVAL 0 HOUR), 3);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @anna, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Обновить README'), 'description', JSON_OBJECT('old', NULL, 'new', 'Описать запуск и примеры запросов.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @gleb)), (@now - INTERVAL 14 DAY - INTERVAL 0 HOUR)),
    (@task, @gleb, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 13 DAY - INTERVAL 0 HOUR)),
    (@task, @gleb, JSON_OBJECT('status', JSON_OBJECT('old', 'in_progress', 'new', 'done'), 'closed_at', JSON_OBJECT('old', NULL, 'new', DATE_FORMAT((@now - INTERVAL 10 DAY - INTERVAL 0 HOUR), '%Y-%m-%dT%H:%i:%s.%fZ'))), (@now - INTERVAL 10 DAY - INTERVAL 0 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Миграция на новую версию Go', 'Обновить образ сборки и go.mod.', 'done', @anna, @boris, (@now - INTERVAL 8 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 5 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 5 DAY - INTERVAL 0 HOUR), 3);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @anna, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Миграция на новую версию Go'), 'description', JSON_OBJECT('old', NULL, 'new', 'Обновить образ сборки и go.mod.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @boris)), (@now - INTERVAL 8 DAY - INTERVAL 0 HOUR)),
    (@task, @boris, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 7 DAY - INTERVAL 0 HOUR)),
    (@task, @boris, JSON_OBJECT('status', JSON_OBJECT('old', 'in_progress', 'new', 'done'), 'closed_at', JSON_OBJECT('old', NULL, 'new', DATE_FORMAT((@now - INTERVAL 5 DAY - INTERVAL 0 HOUR), '%Y-%m-%dT%H:%i:%s.%fZ'))), (@now - INTERVAL 5 DAY - INTERVAL 0 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_dev, 'Планирование спринта', 'Согласовать состав и сроки.', 'todo', @anna, NULL, (@now - INTERVAL 1 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 1 DAY - INTERVAL 0 HOUR), NULL, 1);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @anna, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Планирование спринта'), 'description', JSON_OBJECT('old', NULL, 'new', 'Согласовать состав и сроки.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', NULL)), (@now - INTERVAL 1 DAY - INTERVAL 0 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_mkt, 'Запустить рассылку', 'Письмо о новой версии для клиентов.', 'in_progress', @daria, @vera, (@now - INTERVAL 6 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 5 DAY - INTERVAL 0 HOUR), NULL, 2);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @daria, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Запустить рассылку'), 'description', JSON_OBJECT('old', NULL, 'new', 'Письмо о новой версии для клиентов.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @vera)), (@now - INTERVAL 6 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 5 DAY - INTERVAL 0 HOUR));
INSERT INTO task_comments (task_id, user_id, content, created_at) VALUES
    (@task, @daria, 'Текст согласован.', (@now - INTERVAL 5 DAY - INTERVAL 5 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_mkt, 'Подготовить обзор конкурентов', 'Таблица функций и цен.', 'todo', @daria, NULL, (@now - INTERVAL 3 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 3 DAY - INTERVAL 0 HOUR), NULL, 1);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @daria, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Подготовить обзор конкурентов'), 'description', JSON_OBJECT('old', NULL, 'new', 'Таблица функций и цен.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', NULL)), (@now - INTERVAL 3 DAY - INTERVAL 0 HOUR));

INSERT INTO tasks (team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version) VALUES
    (@team_mkt, 'Обновить лендинг', 'Новые скриншоты и тексты.', 'done', @daria, @vera, (@now - INTERVAL 14 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 4 DAY - INTERVAL 0 HOUR), (@now - INTERVAL 4 DAY - INTERVAL 0 HOUR), 3);
SET @task = LAST_INSERT_ID();
INSERT INTO task_history (task_id, changed_by, changes, created_at) VALUES
    (@task, @daria, JSON_OBJECT('title', JSON_OBJECT('old', NULL, 'new', 'Обновить лендинг'), 'description', JSON_OBJECT('old', NULL, 'new', 'Новые скриншоты и тексты.'), 'status', JSON_OBJECT('old', NULL, 'new', 'todo'), 'assignee_id', JSON_OBJECT('old', NULL, 'new', @vera)), (@now - INTERVAL 14 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'todo', 'new', 'in_progress')), (@now - INTERVAL 13 DAY - INTERVAL 0 HOUR)),
    (@task, @vera, JSON_OBJECT('status', JSON_OBJECT('old', 'in_progress', 'new', 'done'), 'closed_at', JSON_OBJECT('old', NULL, 'new', DATE_FORMAT((@now - INTERVAL 4 DAY - INTERVAL 0 HOUR), '%Y-%m-%dT%H:%i:%s.%fZ'))), (@now - INTERVAL 4 DAY - INTERVAL 0 HOUR));
