package packages

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oxhq/oxinfer/internal/contracts"
	"github.com/oxhq/oxinfer/internal/manifest"
)

type composerLock struct {
	Packages    []composerPackage `json:"packages"`
	PackagesDev []composerPackage `json:"packages-dev"`
}

type composerPackage struct {
	Name     string           `json:"name"`
	Version  string           `json:"version"`
	Autoload composerAutoload `json:"autoload"`
}

type composerManifest struct {
	Require    map[string]string `json:"require"`
	RequireDev map[string]string `json:"require-dev"`
}

type composerAutoload struct {
	PSR4 map[string]any `json:"psr-4"`
}

type packageNamespaceMapping struct {
	PackageName string
	Version     string
	Prefix      string
}

func DetectInstalledPackages(projectRoot string, runtimePackages []contracts.RuntimePackage) ([]DetectedPackage, error) {
	detected := map[string]DetectedPackage{}

	for _, runtimePackage := range runtimePackages {
		name := strings.ToLower(strings.TrimSpace(runtimePackage.Name))
		if !IsSupportedPackage(name) {
			continue
		}

		version := ""
		if runtimePackage.Version != nil {
			version = *runtimePackage.Version
		}
		detected[name] = DetectedPackage{
			Name:    name,
			Version: version,
			Source:  "runtime",
		}
	}

	if err := detectFromComposerLock(filepath.Join(projectRoot, "composer.lock"), detected); err != nil {
		return nil, err
	}
	if err := detectFromComposerManifest(filepath.Join(projectRoot, "composer.json"), detected); err != nil {
		return nil, err
	}

	packages := make([]DetectedPackage, 0, len(detected))
	for _, pkg := range detected {
		packages = append(packages, pkg)
	}
	sortDetectedPackages(packages)
	return packages, nil
}

func DetectRuntimeActionPackages(projectRoot string, routes []contracts.RuntimeRoute) ([]DetectedPackage, error) {
	if len(routes) == 0 {
		return nil, nil
	}

	mappings, err := loadComposerLockNamespaceMappings(filepath.Join(projectRoot, "composer.lock"))
	if err != nil {
		return nil, err
	}
	if len(mappings) == 0 {
		return nil, nil
	}

	detected := make(map[string]DetectedPackage)
	for _, route := range routes {
		if route.Action.FQCN == nil || *route.Action.FQCN == "" {
			continue
		}

		packageName, version := resolvePackageForFQCN(*route.Action.FQCN, mappings)
		if packageName == "" {
			continue
		}

		detected[packageName] = DetectedPackage{
			Name:    packageName,
			Version: version,
			Source:  "runtime.route_action",
		}
	}

	packages := make([]DetectedPackage, 0, len(detected))
	for _, pkg := range detected {
		packages = append(packages, pkg)
	}
	sortDetectedPackages(packages)
	return packages, nil
}

func MergeVendorWhitelist(manifestData *manifest.Manifest, detected []DetectedPackage) {
	if manifestData == nil {
		return
	}

	seen := make(map[string]struct{}, len(manifestData.Scan.VendorWhitelist)+len(detected))
	for _, entry := range manifestData.Scan.VendorWhitelist {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		seen[entry] = struct{}{}
	}

	for _, pkg := range detected {
		if _, ok := seen[pkg.Name]; ok {
			continue
		}
		manifestData.Scan.VendorWhitelist = append(manifestData.Scan.VendorWhitelist, pkg.Name)
		seen[pkg.Name] = struct{}{}
	}

	sort.Strings(manifestData.Scan.VendorWhitelist)
}

func EnsureVendorScanTargets(manifestData *manifest.Manifest) {
	if manifestData == nil || len(manifestData.Scan.VendorWhitelist) == 0 {
		return
	}
	vendorDir := filepath.Join(manifestData.Project.Root, "vendor")
	if info, err := os.Stat(vendorDir); err != nil || !info.IsDir() {
		return
	}

	for _, target := range manifestData.Scan.Targets {
		if strings.EqualFold(strings.TrimSpace(target), "vendor") {
			return
		}
	}

	manifestData.Scan.Targets = append(manifestData.Scan.Targets, "vendor")
}

func detectFromComposerLock(path string, detected map[string]DetectedPackage) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var lock composerLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return err
	}

	for _, pkg := range append(lock.Packages, lock.PackagesDev...) {
		name := strings.ToLower(strings.TrimSpace(pkg.Name))
		if !IsSupportedPackage(name) {
			continue
		}
		existing, ok := detected[name]
		if !ok || existing.Source != "runtime" || existing.Version == "" {
			detected[name] = DetectedPackage{
				Name:    name,
				Version: strings.TrimSpace(pkg.Version),
				Source:  "composer.lock",
			}
		}
	}

	return nil
}

func loadComposerLockNamespaceMappings(path string) ([]packageNamespaceMapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var lock composerLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}

	var mappings []packageNamespaceMapping
	for _, pkg := range append(lock.Packages, lock.PackagesDev...) {
		if pkg.Name == "" || len(pkg.Autoload.PSR4) == 0 {
			continue
		}
		for prefix := range pkg.Autoload.PSR4 {
			prefix = strings.TrimSpace(prefix)
			if prefix == "" {
				continue
			}
			mappings = append(mappings, packageNamespaceMapping{
				PackageName: pkg.Name,
				Version:     strings.TrimSpace(pkg.Version),
				Prefix:      prefix,
			})
		}
	}

	sort.Slice(mappings, func(i, j int) bool {
		if len(mappings[i].Prefix) != len(mappings[j].Prefix) {
			return len(mappings[i].Prefix) > len(mappings[j].Prefix)
		}
		if mappings[i].Prefix != mappings[j].Prefix {
			return mappings[i].Prefix < mappings[j].Prefix
		}
		if mappings[i].PackageName != mappings[j].PackageName {
			return mappings[i].PackageName < mappings[j].PackageName
		}
		return mappings[i].Version < mappings[j].Version
	})

	return mappings, nil
}

func resolvePackageForFQCN(fqcn string, mappings []packageNamespaceMapping) (string, string) {
	normalized := strings.TrimSpace(strings.TrimPrefix(fqcn, `\`))
	if normalized == "" {
		return "", ""
	}

	for _, mapping := range mappings {
		if strings.HasPrefix(normalized, mapping.Prefix) {
			return mapping.PackageName, mapping.Version
		}
	}

	return "", ""
}

func detectFromComposerManifest(path string, detected map[string]DetectedPackage) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var composer composerManifest
	if err := json.Unmarshal(data, &composer); err != nil {
		return err
	}

	merge := func(requirements map[string]string) {
		for name, version := range requirements {
			normalized := strings.ToLower(strings.TrimSpace(name))
			if !IsSupportedPackage(normalized) {
				continue
			}
			existing, ok := detected[normalized]
			if ok && existing.Version != "" {
				continue
			}
			detected[normalized] = DetectedPackage{
				Name:    normalized,
				Version: strings.TrimSpace(version),
				Source:  "composer.json",
			}
		}
	}

	merge(composer.Require)
	merge(composer.RequireDev)
	return nil
}
