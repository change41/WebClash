package config

import (
	"strings"
	"testing"
)

func TestValidateAuthenticationValid(t *testing.T) {
	if err := ValidateAuthentication(nil); err != nil {
		t.Fatalf("empty authentication should be valid: %v", err)
	}
	users := []InboundUser{
		{Username: "alice", Password: "secret"},
		{Username: "bob", Password: "p@ss:with:colon"},
	}
	if err := ValidateAuthentication(users); err != nil {
		t.Fatalf("valid users rejected: %v", err)
	}
}

func TestValidateAuthenticationErrors(t *testing.T) {
	cases := []struct {
		name  string
		users []InboundUser
		want  string
	}{
		{"colon in username", []InboundUser{{Username: "a:b", Password: "x"}}, "不能包含冒号"},
		{"empty password", []InboundUser{{Username: "alice", Password: ""}}, "密码不能为空"},
		{"duplicate username", []InboundUser{
			{Username: "alice", Password: "a"},
			{Username: "alice", Password: "b"},
		}, "重复"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateAuthentication(c.users)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want containing %q", err, c.want)
			}
		})
	}
}
