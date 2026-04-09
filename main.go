package main

import (
	"github.com/fardinabir/go-svc-boilerplate/cmd"
	_ "github.com/fardinabir/go-svc-boilerplate/docs"
)

// @title         Rules Resolution Service
// @version       1.0
// @description   Foreclosure rules resolution engine — resolves step and trait values for a case context using a specificity-based override system.
// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT
// @host          localhost:8082
// @BasePath      /api
// @schemes       http
func main() {
	cmd.Execute()
}
