package controller

import (
	"github.com/coldog/sql-credential-operator/pkg/controller/sqlcredential"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, sqlcredential.Add)
}
