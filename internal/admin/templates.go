package admin

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/template/html/v3"
	"github.com/samber/oops"
)

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

func NewTemplateEngine(messages *Messages) (fiber.Views, error) {
	if messages == nil {
		var err error
		messages, err = NewMessages()
		if err != nil {
			return nil, err
		}
	}
	templates, err := fs.Sub(templateFS, "templates")
	if err != nil {
		return nil, oops.In("admin").Wrapf(err, "open embedded admin templates")
	}
	engine := html.NewFileSystem(http.FS(templates), ".html")
	engine.AddFunc("t", messages.TemplateTranslate)
	return engine, nil
}
