package koba

import (
	"github.com/roxiegrillo/koba/v2/middleware"
)

func MakeRawApp() KobaApp {
	app := kobaApp{}
	app.startApp()

	app.Use(&middleware.WithHttpContext{})
	return &app
}

func MakeDefaultKobaApp() KobaApp {
	app := kobaApp{}

	app.startApp()
	app.Use(&middleware.WithHttpContext{})
	return &app
}
