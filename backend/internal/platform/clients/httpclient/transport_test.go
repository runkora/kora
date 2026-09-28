package httpclient

import (
	"net/url"
	"testing"
)

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain URL is unchanged",
			in:   "https://mcp.example.com/tools",
			want: "https://mcp.example.com/tools",
		},
		{
			name: "userinfo is stripped",
			in:   "https://user:s3cr3t@mcp.example.com/tools",
			want: "https://mcp.example.com/tools",
		},
		{
			name: "query string is redacted",
			in:   "https://mcp.example.com/tools?api_key=s3cr3t",
			want: "https://mcp.example.com/tools?REDACTED",
		},
		{
			name: "userinfo and query both redacted",
			in:   "https://user:s3cr3t@mcp.example.com/tools?token=abc123",
			want: "https://mcp.example.com/tools?REDACTED",
		},
		{
			name: "fragment is dropped",
			in:   "https://mcp.example.com/tools#s3cr3t",
			want: "https://mcp.example.com/tools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.in)
			if err != nil {
				t.Fatalf("parse %q: %v", tt.in, err)
			}
			got := redactURL(u)
			if got != tt.want {
				t.Errorf("redactURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// The original URL must not be mutated (redactURL takes a copy).
			if u.String() != tt.in {
				t.Errorf("redactURL mutated input: got %q, want unchanged %q", u.String(), tt.in)
			}
		})
	}
}
