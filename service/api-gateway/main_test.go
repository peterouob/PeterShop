package main

import (
	"testing"

	"go.uber.org/fx"
)

func TestAppGraph(t *testing.T) {
	if err := fx.ValidateApp(app, fx.NopLogger); err != nil {
		t.Fatal(err)
	}
}
