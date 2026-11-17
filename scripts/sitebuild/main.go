package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultRepo = "0xBB2B/codexx"

var (
	versionPattern  = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	templatePattern = regexp.MustCompile(`(?s)<template data-sb="([\w-]+)">(.*?)</template>\n?`)
	regionPattern   = regexp.MustCompile(`(?s)<!--sb:([\w-]+)-->.*?<!--/sb-->`)
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "用法: sitebuild <源目录> <输出目录> <版本号或空串>")
		os.Exit(2)
	}
	src, out, version := os.Args[1], os.Args[2], os.Args[3]
	if version != "" && !versionPattern.MatchString(version) {
		fmt.Fprintf(os.Stderr, "版本号 %q 必须是 X.Y.Z\n", version)
		os.Exit(2)
	}
	if err := generate(src, out, version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(src, out, version string) error {
	if err := copyDir(src, out); err != nil {
		return err
	}
	if version == "" {
		return nil
	}
	index := filepath.Join(out, "index.html")
	page, err := os.ReadFile(index)
	if err != nil {
		return err
	}
	released, err := release(string(page), version)
	if err != nil {
		return err
	}
	return os.WriteFile(index, []byte(released), 0o644)
}

func release(page, version string) (string, error) {
	base := "https://github.com/" + defaultRepo + "/releases/download/v" + version + "/model-bridge-" + version
	fill := strings.NewReplacer(
		"{{ver}}", version,
		"{{mac}}", base+"-macos.dmg",
		"{{win}}", base+"-windows-setup.exe",
	)
	templates := map[string]string{}
	for _, m := range templatePattern.FindAllStringSubmatch(page, -1) {
		templates[m[1]] = fill.Replace(m[2])
	}
	page = templatePattern.ReplaceAllString(page, "")
	var missing []string
	page = regionPattern.ReplaceAllStringFunc(page, func(region string) string {
		name := regionPattern.FindStringSubmatch(region)[1]
		body, ok := templates[name]
		if !ok {
			missing = append(missing, name)
		}
		return body
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("缺少已发布片段: %v", missing)
	}
	return page, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
