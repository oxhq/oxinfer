package packages

import (
	"sort"
	"strings"
)

const (
	PackageSpatieLaravelData         = "spatie/laravel-data"
	PackageSpatieLaravelQueryBuilder = "spatie/laravel-query-builder"
	PackageSpatieLaravelPermission   = "spatie/laravel-permission"
	PackageSpatieLaravelMediaLibrary = "spatie/laravel-medialibrary"
	PackageSpatieLaravelTranslatable = "spatie/laravel-translatable"
)

type DetectedPackage struct {
	Name    string
	Version string
	Source  string
}

var supportedPackages = map[string]struct{}{
	PackageSpatieLaravelData:         {},
	PackageSpatieLaravelQueryBuilder: {},
	PackageSpatieLaravelPermission:   {},
	PackageSpatieLaravelMediaLibrary: {},
	PackageSpatieLaravelTranslatable: {},
}

func IsSupportedPackage(name string) bool {
	_, ok := supportedPackages[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func sortDetectedPackages(packages []DetectedPackage) {
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Name != packages[j].Name {
			return packages[i].Name < packages[j].Name
		}
		if packages[i].Version != packages[j].Version {
			return packages[i].Version < packages[j].Version
		}
		return packages[i].Source < packages[j].Source
	})
}
