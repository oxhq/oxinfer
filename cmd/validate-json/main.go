package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "io/ioutil"
    "os"
    "path/filepath"

    jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
)

func main() {
    schemaPath := flag.String("schema", "", "Path to JSON Schema file")
    jsonPath := flag.String("json", "", "Path to JSON document to validate")
    flag.Parse()

    if *schemaPath == "" || *jsonPath == "" {
        fmt.Fprintln(os.Stderr, "usage: validate-json -schema path/to/schema.json -json path/to/document.json")
        os.Exit(2)
    }

    // Compile schema from absolute path
    compiler := jsonschema.NewCompiler()
    compiler.Draft = jsonschema.Draft2020
    abs, err := filepath.Abs(*schemaPath)
    if err != nil {
        fmt.Fprintln(os.Stderr, "failed to resolve schema path:", err)
        os.Exit(1)
    }
    schema, err := compiler.Compile("file://" + abs)
    if err != nil {
        fmt.Fprintln(os.Stderr, "failed to compile schema:", err)
        os.Exit(1)
    }

    // Load JSON document
    dataBytes, err := ioutil.ReadFile(*jsonPath)
    if err != nil {
        fmt.Fprintln(os.Stderr, "failed to read json:", err)
        os.Exit(1)
    }
    var data interface{}
    if err := json.Unmarshal(dataBytes, &data); err != nil {
        fmt.Fprintln(os.Stderr, "invalid json:", err)
        os.Exit(1)
    }

    // Validate
    if err := schema.Validate(data); err != nil {
        fmt.Fprintln(os.Stderr, "validation failed:")
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }

    fmt.Println("OK")
}
