// Спецификация API и страница Swagger UI. openapi.yaml написан вручную и
// встроен в бинарник, поэтому версия документации всегда совпадает с версией
// сервиса. Swagger UI подключается с CDN строго фиксированной версии и с
// проверкой хеша (SRI): если файл на CDN изменится, браузер его не выполнит.
// Хеши посчитаны по swagger-ui-dist@5.17.14; при смене версии их нужно
// пересчитать.

package api

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.yaml
var Spec []byte

const SwaggerUIVersion = "5.17.14"

const uiPage = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Task Service API</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@` + SwaggerUIVersion + `/swagger-ui.css" integrity="sha384-wxLW6kwyHktdDGr6Pv1zgm/VGJh99lfUbzSn6HNHBENZlCN7W602k9VkGdxuFvPn" crossorigin="anonymous">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@` + SwaggerUIVersion + `/swagger-ui-bundle.js" integrity="sha384-wmyclcVGX/WhUkdkATwhaK1X1JtiNrr2EoYJ+diV3vj4v6OC5yCeSu+yW13SYJep" crossorigin="anonymous"></script>
<script>
window.onload = function () {
  window.ui = SwaggerUIBundle({
    url: "./openapi.yaml",
    dom_id: "#swagger-ui",
    deepLinking: true,
    persistAuthorization: true,
    displayRequestDuration: true
  });
};
</script>
</body>
</html>
`

func SpecHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
		_, _ = w.Write(Spec)
	}
}

func UIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(uiPage))
	}
}
