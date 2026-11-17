package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var tagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

func main() {
	if len(os.Args) != 3 {
		fail("用法：releasetag <标签> <基线引用>")
	}
	tag, base := os.Args[1], os.Args[2]
	if !tagPattern.MatchString(tag) {
		fail(fmt.Sprintf("标签 %q 不是 vX.Y.Z 或 vX.Y.Z-后缀 格式", tag))
	}
	if err := exec.Command("git", "merge-base", "--is-ancestor", tag+"^{commit}", base).Run(); err != nil {
		fail(fmt.Sprintf("标签 %s 所指提交不在 %s 上", tag, base))
	}
	fmt.Printf("version=%s\nprerelease=%t\n", strings.TrimPrefix(tag, "v"), strings.Contains(tag, "-"))
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
