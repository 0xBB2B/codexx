package version

import "testing"

func TestCurrent(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "未注入版本号时返回开发版本", want: "0.0.0-dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Current(); got != tt.want {
				t.Errorf("Current() = %q, want %q", got, tt.want)
			}
		})
	}
}
