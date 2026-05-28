package admin

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	"github.com/samber/oops"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

func NewTemplateEngine() (fiber.Views, error) {
	if _, err := loadMessages(); err != nil {
		return nil, err
	}
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		return nil, oops.In("admin").Wrapf(err, "open embedded admin templates")
	}
	engine := html.NewFileSystem(http.FS(templates), ".html")
	engine.AddFunc("t", templateTranslate)
	return engine, nil
}
