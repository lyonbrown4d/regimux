package admin

import (
	"embed"
	"io/fs"
	"net/http"
	"reflect"

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
	engine.AddFunc("values", func(value any) any {
		if value == nil {
			return nil
		}
		method := reflect.ValueOf(value).MethodByName("Values")
		if !method.IsValid() {
			return value
		}
		if method.Type().NumIn() != 0 || method.Type().NumOut() != 1 {
			return value
		}
		result := method.Call(nil)
		return result[0].Interface()
	})
	return engine, nil
}
