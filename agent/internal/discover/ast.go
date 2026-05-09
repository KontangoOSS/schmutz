package discover

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	golang "github.com/smacker/go-tree-sitter/golang"
)

type RouteHint struct {
	Method  string
	Path    string
	Handler string
	File    string
	Line    uint32
}

func ExtractGoRoutes(sourceDir string) ([]RouteHint, error) {
	parser := sitter.NewParser()
	parser.SetLanguage(golang.GetLanguage())

	var routes []RouteHint

	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, "/vendor/") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		tree, err := parser.ParseCtx(context.Background(), nil, src)
		if err != nil {
			return nil
		}
		defer tree.Close()

		routes = append(routes, extractRoutesFromTree(tree.RootNode(), src, path)...)
		return nil
	})

	return routes, err
}

func extractRoutesFromTree(root *sitter.Node, src []byte, file string) []RouteHint {
	var hints []RouteHint

	query := `
(call_expression
  function: (selector_expression
    operand: (identifier) @receiver
    field: (field_identifier) @method)
  arguments: (argument_list
    (interpreted_string_literal) @path
    .
    (_) @handler))
`
	q, err := sitter.NewQuery([]byte(query), golang.GetLanguage())
	if err != nil {
		return nil
	}
	defer q.Close()

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()
	cursor.Exec(q, root)

	httpMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "DELETE": true,
		"PATCH": true, "HEAD": true, "OPTIONS": true,
		"Handle": true, "HandleFunc": true, "Route": true, "Group": true, "Any": true,
	}

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		var method, path string
		for _, cap := range match.Captures {
			name := q.CaptureNameForId(cap.Index)
			val := string(src[cap.Node.StartByte():cap.Node.EndByte()])
			switch name {
			case "method":
				if httpMethods[strings.ToUpper(val)] || httpMethods[val] {
					method = strings.ToUpper(val)
				}
			case "path":
				if len(val) >= 2 {
					path = val[1 : len(val)-1]
				}
			}
		}

		if method != "" && path != "" && strings.HasPrefix(path, "/") {
			hints = append(hints, RouteHint{
				Method: method,
				Path:   path,
				File:   file,
				Line:   match.Captures[0].Node.StartPoint().Row + 1,
			})
		}
	}

	return hints
}

func RoutesToOpenAPIPaths(routes []RouteHint) map[string]interface{} {
	paths := make(map[string]interface{})
	for _, r := range routes {
		method := strings.ToLower(r.Method)
		if method == "handlefunc" || method == "handle" {
			method = "get"
		}
		pathItem, ok := paths[r.Path].(map[string]interface{})
		if !ok {
			pathItem = make(map[string]interface{})
		}
		pathItem[method] = map[string]interface{}{
			"summary": fmt.Sprintf("%s %s", r.Method, r.Path),
			"responses": map[string]interface{}{
				"200": map[string]interface{}{"description": "Success"},
			},
			"x-source-file": r.File,
			"x-source-line": r.Line,
		}
		paths[r.Path] = pathItem
	}
	return paths
}
