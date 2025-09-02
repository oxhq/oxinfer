// Package production provides test quality auditing for the Oxinfer project.
// This module identifies and categorizes tests to ensure meaningful coverage.
package production

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TestQuality represents the quality assessment of a test.
type TestQuality int

const (
	TestQualityMeaningful TestQuality = iota // Tests real business logic
	TestQualityMarginal                      // Provides some value but could be improved
	TestQualityArtificial                    // Exists primarily for coverage
	TestQualityStub                          // Placeholder with no real validation
)

// TestCategory represents the type of test.
type TestCategory int

const (
	TestCategoryUnit TestCategory = iota
	TestCategoryIntegration
	TestCategoryEndToEnd
	TestCategoryBenchmark
	TestCategoryExample
)

// TestAnalysis represents the analysis of a single test.
type TestAnalysis struct {
	FilePath     string
	TestName     string
	Category     TestCategory
	Quality      TestQuality
	LineCount    int
	MockUsage    int
	Assertions   int
	RealIO       bool
	BusinessLogic bool
	Issues       []string
	Recommendations []string
}

// QualityAudit represents the complete test quality audit.
type QualityAudit struct {
	TestFiles       []string
	TestAnalyses    []*TestAnalysis
	QualityBreakdown map[TestQuality]int
	CategoryBreakdown map[TestCategory]int
	TotalTests      int
	MeaningfulTests int
	ArtificialTests int
	Coverage        float64
	Recommendations []string
}

// AuditTestQuality performs a comprehensive audit of all tests in the project.
func AuditTestQuality(projectRoot string) (*QualityAudit, error) {
	audit := &QualityAudit{
		QualityBreakdown: make(map[TestQuality]int),
		CategoryBreakdown: make(map[TestCategory]int),
	}

	// Find all test files
	err := filepath.Walk(projectRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if strings.HasSuffix(info.Name(), "_test.go") {
			audit.TestFiles = append(audit.TestFiles, path)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk project: %w", err)
	}

	// Analyze each test file
	for _, testFile := range audit.TestFiles {
		analyses, err := analyzeTestFile(testFile)
		if err != nil {
			return nil, fmt.Errorf("failed to analyze %s: %w", testFile, err)
		}

		audit.TestAnalyses = append(audit.TestAnalyses, analyses...)
	}

	// Calculate statistics
	audit.calculateStatistics()
	audit.generateRecommendations()

	return audit, nil
}

// analyzeTestFile analyzes a single test file and returns all test analyses.
func analyzeTestFile(filePath string) ([]*TestAnalysis, error) {
	// Parse the Go file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file: %w", err)
	}

	var analyses []*TestAnalysis

	// Walk the AST to find test functions
	ast.Inspect(node, func(n ast.Node) bool {
		if fn, ok := n.(*ast.FuncDecl); ok {
			if isTestFunction(fn) {
				analysis := analyzeTestFunction(filePath, fn, fset)
				analyses = append(analyses, analysis)
			}
		}
		return true
	})

	return analyses, nil
}

// isTestFunction determines if a function is a test function.
func isTestFunction(fn *ast.FuncDecl) bool {
	if fn.Name == nil {
		return false
	}

	name := fn.Name.Name
	return strings.HasPrefix(name, "Test") || 
		   strings.HasPrefix(name, "Benchmark") ||
		   strings.HasPrefix(name, "Example")
}

// analyzeTestFunction analyzes a single test function.
func analyzeTestFunction(filePath string, fn *ast.FuncDecl, fset *token.FileSet) *TestAnalysis {
	analysis := &TestAnalysis{
		FilePath: filePath,
		TestName: fn.Name.Name,
	}

	// Determine category
	analysis.Category = determineTestCategory(fn.Name.Name, filePath)

	// Count lines
	start := fset.Position(fn.Pos()).Line
	end := fset.Position(fn.End()).Line
	analysis.LineCount = end - start + 1

	// Analyze function body
	if fn.Body != nil {
		analyzeTestBody(analysis, fn.Body)
	}

	// Determine quality based on analysis
	analysis.Quality = determineTestQuality(analysis)

	return analysis
}

// determineTestCategory determines the category of a test.
func determineTestCategory(testName, filePath string) TestCategory {
	if strings.HasPrefix(testName, "Benchmark") {
		return TestCategoryBenchmark
	}

	if strings.HasPrefix(testName, "Example") {
		return TestCategoryExample
	}

	if strings.Contains(filePath, "integration") || 
	   strings.Contains(filePath, "e2e") {
		return TestCategoryIntegration
	}

	if strings.Contains(testName, "Integration") ||
	   strings.Contains(testName, "E2E") ||
	   strings.Contains(testName, "EndToEnd") {
		return TestCategoryIntegration
	}

	return TestCategoryUnit
}

// analyzeTestBody analyzes the body of a test function.
func analyzeTestBody(analysis *TestAnalysis, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			analyzeCallExpression(analysis, node)
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				// Check for file paths, indicating real I/O
				value := strings.Trim(node.Value, `"'`)
				if strings.Contains(value, ".php") || 
				   strings.Contains(value, ".json") ||
				   strings.Contains(value, "/tmp/") {
					analysis.RealIO = true
				}
			}
		}
		return true
	})
}

// analyzeCallExpression analyzes a function call within a test.
func analyzeCallExpression(analysis *TestAnalysis, call *ast.CallExpr) {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		funcName := sel.Sel.Name

		// Count test assertions
		if isAssertionFunction(funcName) {
			analysis.Assertions++
		}

		// Check for mock usage
		if isMockFunction(funcName) {
			analysis.MockUsage++
		}

		// Check for business logic
		if isBusinessLogicFunction(funcName) {
			analysis.BusinessLogic = true
		}
	}

	// Check for direct function calls
	if ident, ok := call.Fun.(*ast.Ident); ok {
		funcName := ident.Name

		if isAssertionFunction(funcName) {
			analysis.Assertions++
		}

		if strings.Contains(funcName, "Mock") || 
		   strings.Contains(funcName, "Stub") {
			analysis.MockUsage++
		}
	}
}

// isAssertionFunction determines if a function is a test assertion.
func isAssertionFunction(funcName string) bool {
	assertions := []string{
		"Equal", "NotEqual", "True", "False", "Nil", "NotNil",
		"Error", "NoError", "Contains", "NotContains",
		"Greater", "Less", "GreaterOrEqual", "LessOrEqual",
		"Len", "Empty", "NotEmpty", "Zero", "NotZero",
		"Fatalf", "Errorf", "Logf",
	}

	for _, assertion := range assertions {
		if funcName == assertion || strings.HasSuffix(funcName, assertion) {
			return true
		}
	}

	return false
}

// isMockFunction determines if a function is mock-related.
func isMockFunction(funcName string) bool {
	mockKeywords := []string{
		"Mock", "Stub", "Fake", "Mock", "ExpectCall",
		"Return", "DoAndReturn", "Times",
	}

	for _, keyword := range mockKeywords {
		if strings.Contains(funcName, keyword) {
			return true
		}
	}

	return false
}

// isBusinessLogicFunction determines if a function represents business logic.
func isBusinessLogicFunction(funcName string) bool {
	businessFunctions := []string{
		"ProcessProject", "IndexFiles", "ParsePHP", "MatchPatterns",
		"InferShape", "EmitDelta", "LoadManifest", "ValidateManifest",
		"ResolveNamespace", "ExtractController", "DetectPivot",
	}

	for _, business := range businessFunctions {
		if strings.Contains(funcName, business) {
			return true
		}
	}

	return false
}

// determineTestQuality determines the quality of a test based on analysis.
func determineTestQuality(analysis *TestAnalysis) TestQuality {
	// Stub tests: very short, no real assertions
	if analysis.LineCount <= 5 && analysis.Assertions <= 1 {
		analysis.Issues = append(analysis.Issues, "Very short test with minimal assertions")
		return TestQualityStub
	}

	// Artificial tests: high mock usage, no real I/O or business logic
	if analysis.MockUsage > analysis.Assertions && 
	   !analysis.RealIO && 
	   !analysis.BusinessLogic {
		analysis.Issues = append(analysis.Issues, "High mock usage without testing real functionality")
		return TestQualityArtificial
	}

	// Marginal tests: some value but could be improved
	if analysis.Assertions < 3 || 
	   (analysis.MockUsage > 0 && !analysis.BusinessLogic) {
		if analysis.Assertions < 3 {
			analysis.Issues = append(analysis.Issues, "Few assertions may not catch regressions")
		}
		if analysis.MockUsage > 0 && !analysis.BusinessLogic {
			analysis.Issues = append(analysis.Issues, "Uses mocks but doesn't test business logic")
		}
		return TestQualityMarginal
	}

	// Meaningful tests: good assertions, tests real functionality
	if analysis.BusinessLogic || analysis.RealIO {
		return TestQualityMeaningful
	}

	// Default to marginal
	return TestQualityMarginal
}

// calculateStatistics calculates audit statistics.
func (audit *QualityAudit) calculateStatistics() {
	audit.TotalTests = len(audit.TestAnalyses)

	for _, analysis := range audit.TestAnalyses {
		audit.QualityBreakdown[analysis.Quality]++
		audit.CategoryBreakdown[analysis.Category]++

		if analysis.Quality == TestQualityMeaningful {
			audit.MeaningfulTests++
		}

		if analysis.Quality == TestQualityArtificial || 
		   analysis.Quality == TestQualityStub {
			audit.ArtificialTests++
		}
	}

	// Calculate meaningful coverage percentage
	if audit.TotalTests > 0 {
		audit.Coverage = float64(audit.MeaningfulTests) / float64(audit.TotalTests) * 100
	}
}

// generateRecommendations generates improvement recommendations.
func (audit *QualityAudit) generateRecommendations() {
	artificialPercentage := float64(audit.ArtificialTests) / float64(audit.TotalTests) * 100

	if artificialPercentage > 30 {
		audit.Recommendations = append(audit.Recommendations,
			fmt.Sprintf("High artificial test percentage (%.1f%%) - focus on testing real functionality", artificialPercentage))
	}

	if audit.Coverage < 50 {
		audit.Recommendations = append(audit.Recommendations,
			fmt.Sprintf("Low meaningful coverage (%.1f%%) - add tests that validate business logic", audit.Coverage))
	}

	stubTests := audit.QualityBreakdown[TestQualityStub]
	if stubTests > 0 {
		audit.Recommendations = append(audit.Recommendations,
			fmt.Sprintf("Remove %d stub tests that provide no value", stubTests))
	}

	// Analyze specific test issues
	issueCategories := make(map[string]int)
	for _, analysis := range audit.TestAnalyses {
		for _, issue := range analysis.Issues {
			issueCategories[issue]++
		}
	}

	// Sort issues by frequency
	type issueCount struct {
		issue string
		count int
	}

	var issues []issueCount
	for issue, count := range issueCategories {
		issues = append(issues, issueCount{issue, count})
	}

	sort.Slice(issues, func(i, j int) bool {
		return issues[i].count > issues[j].count
	})

	// Add top issues as recommendations
	for i, issue := range issues {
		if i >= 3 { // Only top 3 issues
			break
		}
		if issue.count > 1 {
			audit.Recommendations = append(audit.Recommendations,
				fmt.Sprintf("Address common issue: %s (affects %d tests)", issue.issue, issue.count))
		}
	}
}

// PrintReport prints a comprehensive test quality report.
func (audit *QualityAudit) PrintReport() {
	fmt.Println("\n=== TEST QUALITY AUDIT REPORT ===")
	fmt.Printf("Total Tests: %d\n", audit.TotalTests)
	fmt.Printf("Meaningful Tests: %d (%.1f%%)\n", audit.MeaningfulTests, audit.Coverage)
	fmt.Printf("Artificial/Stub Tests: %d (%.1f%%)\n", 
		audit.ArtificialTests, 
		float64(audit.ArtificialTests)/float64(audit.TotalTests)*100)

	fmt.Println("\n=== QUALITY BREAKDOWN ===")
	qualityNames := map[TestQuality]string{
		TestQualityMeaningful: "Meaningful",
		TestQualityMarginal:   "Marginal",
		TestQualityArtificial: "Artificial",
		TestQualityStub:       "Stub",
	}

	for quality := TestQualityMeaningful; quality <= TestQualityStub; quality++ {
		count := audit.QualityBreakdown[quality]
		percentage := float64(count) / float64(audit.TotalTests) * 100
		fmt.Printf("  %s: %d (%.1f%%)\n", qualityNames[quality], count, percentage)
	}

	fmt.Println("\n=== CATEGORY BREAKDOWN ===")
	categoryNames := map[TestCategory]string{
		TestCategoryUnit:        "Unit",
		TestCategoryIntegration: "Integration",
		TestCategoryEndToEnd:    "End-to-End",
		TestCategoryBenchmark:   "Benchmark",
		TestCategoryExample:     "Example",
	}

	for category := TestCategoryUnit; category <= TestCategoryExample; category++ {
		count := audit.CategoryBreakdown[category]
		if count > 0 {
			percentage := float64(count) / float64(audit.TotalTests) * 100
			fmt.Printf("  %s: %d (%.1f%%)\n", categoryNames[category], count, percentage)
		}
	}

	fmt.Println("\n=== RECOMMENDATIONS ===")
	if len(audit.Recommendations) == 0 {
		fmt.Println("  No major issues found - test quality looks good!")
	} else {
		for i, rec := range audit.Recommendations {
			fmt.Printf("  %d. %s\n", i+1, rec)
		}
	}

	fmt.Println("\n=== PROBLEMATIC TESTS ===")
	problemTests := 0
	for _, analysis := range audit.TestAnalyses {
		if analysis.Quality == TestQualityArtificial || 
		   analysis.Quality == TestQualityStub ||
		   len(analysis.Issues) > 0 {
			
			if problemTests == 0 {
				fmt.Println("Tests that need improvement:")
			}
			problemTests++

			fmt.Printf("  %s:%s\n", filepath.Base(analysis.FilePath), analysis.TestName)
			fmt.Printf("    Quality: %s\n", qualityNames[analysis.Quality])
			for _, issue := range analysis.Issues {
				fmt.Printf("    Issue: %s\n", issue)
			}
			fmt.Println()

			if problemTests >= 10 { // Limit output
				remaining := 0
				for _, a := range audit.TestAnalyses {
					if a.Quality == TestQualityArtificial || a.Quality == TestQualityStub {
						remaining++
					}
				}
				remaining -= problemTests
				if remaining > 0 {
					fmt.Printf("  ... and %d more tests with issues\n", remaining)
				}
				break
			}
		}
	}

	if problemTests == 0 {
		fmt.Println("  No problematic tests found!")
	}

	fmt.Printf("\n=== OVERALL ASSESSMENT ===\n")
	if audit.Coverage >= 70 && audit.ArtificialTests < audit.TotalTests/4 {
		fmt.Println("TEST QUALITY: GOOD - Tests focus on meaningful functionality")
	} else if audit.Coverage >= 50 && audit.ArtificialTests < audit.TotalTests/2 {
		fmt.Println("TEST QUALITY: FAIR - Some improvements needed")
	} else {
		fmt.Println("TEST QUALITY: POOR - Major improvements needed")
		fmt.Println("Focus on testing real business logic instead of artificial coverage")
	}
}

// GetArtificialTests returns a list of artificial/stub tests that should be removed.
func (audit *QualityAudit) GetArtificialTests() []*TestAnalysis {
	var artificial []*TestAnalysis
	
	for _, analysis := range audit.TestAnalyses {
		if analysis.Quality == TestQualityArtificial || 
		   analysis.Quality == TestQualityStub {
			artificial = append(artificial, analysis)
		}
	}

	return artificial
}

// GetMeaningfulTests returns a list of meaningful tests that provide real value.
func (audit *QualityAudit) GetMeaningfulTests() []*TestAnalysis {
	var meaningful []*TestAnalysis
	
	for _, analysis := range audit.TestAnalyses {
		if analysis.Quality == TestQualityMeaningful {
			meaningful = append(meaningful, analysis)
		}
	}

	return meaningful
}