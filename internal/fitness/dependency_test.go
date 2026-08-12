package fitness

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

var forbiddenImports = []string{
	"dummypay/internal/http",
	"dummypay/internal/postgres",
	"dummypay/internal/webhook",
	"github.com/go-chi/chi",
	"github.com/jackc/pgx",
	"net/http",
}

func TestDependencyDirection_PaymentDoesNotImportAdapters(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedImports,
	}, "dummypay/internal/payment")
	if err != nil {
		t.Fatalf("load payment package: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("payment package not found")
	}
	pkg := pkgs[0]

	for _, forbidden := range forbiddenImports {
		if _, ok := pkg.Imports[forbidden]; ok {
			t.Errorf("internal/payment imports %q — it must not import any adapter, driver, or web framework (ADR-0003)", forbidden)
		}
	}

	for _, pkgErr := range pkg.Errors {
		t.Errorf("package error: %v", pkgErr)
	}
}

func TestDependencyDirection_WrongImportFails(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedImports,
	}, "dummypay/internal/payment")
	if err != nil {
		t.Fatalf("load payment package: %v", err)
	}
	if len(pkgs) == 0 {
		t.Fatal("payment package not found")
	}
	pkg := pkgs[0]

	for importPath := range pkg.Imports {
		if strings.Contains(importPath, "chi") ||
			strings.Contains(importPath, "pgx") ||
			strings.Contains(importPath, "gin") ||
			strings.Contains(importPath, "echo") {
			t.Errorf("internal/payment imports %q — must not import any web framework or database driver", importPath)
		}
	}
}
