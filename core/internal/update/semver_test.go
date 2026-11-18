package update_test

import (
	"testing"

	"modelbridge/internal/update"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  update.Version
		valid bool
	}{
		{name: "带 v 前缀", in: "v1.3.0", want: update.Version{1, 3, 0}, valid: true},
		{name: "不带前缀", in: "1.3.0", want: update.Version{1, 3, 0}, valid: true},
		{name: "多位数字", in: "v10.20.30", want: update.Version{10, 20, 30}, valid: true},
		{name: "预发布后缀", in: "1.3.0-beta", valid: false},
		{name: "带 v 的预发布后缀", in: "v1.3.0-rc.1", valid: false},
		{name: "构建元数据", in: "1.3.0+build5", valid: false},
		{name: "只有两段", in: "1.3", valid: false},
		{name: "四段", in: "1.3.0.1", valid: false},
		{name: "空串", in: "", valid: false},
		{name: "只有 v", in: "v", valid: false},
		{name: "两个 v", in: "vv1.3.0", valid: false},
		{name: "大写 V", in: "V1.3.0", valid: false},
		{name: "非数字", in: "a.b.c", valid: false},
		{name: "负数", in: "1.-3.0", valid: false},
		{name: "带加号", in: "1.+3.0", valid: false},
		{name: "空段", in: "1..0", valid: false},
		{name: "首尾空白", in: " 1.3.0", valid: false},
		{name: "数字溢出", in: "99999999999999999999.0.0", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := update.ParseVersion(tt.in)
			if ok != tt.valid {
				t.Fatalf("ParseVersion(%q) ok = %v, want %v", tt.in, ok, tt.valid)
			}
			if ok && got != tt.want {
				t.Errorf("ParseVersion(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestVersion_Newer(t *testing.T) {
	tests := []struct {
		name string
		a, b update.Version
		want bool
	}{
		{name: "修订号更大", a: update.Version{1, 2, 1}, b: update.Version{1, 2, 0}, want: true},
		{name: "次版本更大", a: update.Version{1, 3, 0}, b: update.Version{1, 2, 9}, want: true},
		{name: "主版本更大", a: update.Version{2, 0, 0}, b: update.Version{1, 99, 99}, want: true},
		{name: "相等", a: update.Version{1, 2, 0}, b: update.Version{1, 2, 0}, want: false},
		{name: "修订号更小", a: update.Version{1, 1, 9}, b: update.Version{1, 2, 0}, want: false},
		{name: "按数字而非字符串比较", a: update.Version{1, 10, 0}, b: update.Version{1, 9, 0}, want: true},
		{name: "按数字比较反向", a: update.Version{1, 9, 0}, b: update.Version{1, 10, 0}, want: false},
		{name: "主版本更小但次版本更大", a: update.Version{1, 9, 0}, b: update.Version{2, 0, 0}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Newer(tt.b); got != tt.want {
				t.Errorf("%v.Newer(%v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
