package main

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveUserHome(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatalf("failed get the current user: %v", err)
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "home is expanded",
			path: "~/.ssh/id_rsa",
			want: filepath.Join(current.HomeDir, ".ssh", "id_rsa"),
		},
		{
			name: "an absolute path is kept",
			path: "/etc/mogura/config.yml",
			want: "/etc/mogura/config.yml",
		},
		{
			name: "a relative path is kept",
			path: "./local-env/known_hosts",
			want: "./local-env/known_hosts",
		},
		{
			name: "a tilde that is not the home prefix is kept",
			path: "/tmp/~backup/config.yml",
			want: "/tmp/~backup/config.yml",
		},
		{
			name: "an empty path is kept",
			path: "",
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Act
			got, err := ResolveUserHome(c.path)

			// Assert
			if err != nil {
				t.Fatalf("ResolveUserHome failed: %v", err)
			}
			if got != c.want {
				t.Errorf("want %q, got %q", c.want, got)
			}
		})
	}
}

// TestGetDefaultConfigPath covers the path mogura reads without -config. it
// used to be built from the HOME environment variable, which is not set on
// windows and leaves the path at /.mogura/config.yml when it is missing.
func TestGetDefaultConfigPath(t *testing.T) {
	// Arrange
	current, err := user.Current()
	if err != nil {
		t.Fatalf("failed get the current user: %v", err)
	}

	// Act
	got, err := GetDefaultConfigPath()

	// Assert
	if err != nil {
		t.Fatalf("GetDefaultConfigPath failed: %v", err)
	}

	want := filepath.Join(current.HomeDir, ".mogura", "config.yml")
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("want an absolute path, got %q", got)
	}
}

func TestLoadConfig(t *testing.T) {
	// Arrange
	path := filepath.Join(t.TempDir(), "config.yml")
	content := `bastion_ssh_config:
  name: bastion
  host: bastion.example.com
  port: 2222
  user: ec2-user
  key_path: ~/ssh_key.pem
  known_hosts_path: ~/.ssh/known_hosts
  insecure_ignore_host_key: true
  remote_dns: 10.0.0.2:53
tunnels:
  - name: nginx
    local_bind_port: 8080
    target: web.internal
    target_port: 80
    forwarding_timeout: 10s
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed write config: %v", err)
	}

	// Act
	c, err := LoadConfig(path)

	// Assert
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// the bastion section is read through a struct tag that has been wrong
	// before, and a wrong tag leaves every field at its zero value.
	if c.Bastion.Host != "bastion.example.com" {
		t.Errorf("want host bastion.example.com, got %q", c.Bastion.Host)
	}
	if c.Bastion.Port != 2222 {
		t.Errorf("want port 2222, got %d", c.Bastion.Port)
	}
	if c.Bastion.User != "ec2-user" {
		t.Errorf("want user ec2-user, got %q", c.Bastion.User)
	}
	if c.Bastion.KnownHostsPath != "~/.ssh/known_hosts" {
		t.Errorf("want known_hosts_path ~/.ssh/known_hosts, got %q", c.Bastion.KnownHostsPath)
	}
	if !c.Bastion.InsecureIgnoreHostKey {
		t.Error("want insecure_ignore_host_key true, got false")
	}

	if len(c.Tunnels) != 1 {
		t.Fatalf("want 1 tunnel, got %d", len(c.Tunnels))
	}
	if c.Tunnels[0].LocalBindPort != 8080 {
		t.Errorf("want local_bind_port 8080, got %d", c.Tunnels[0].LocalBindPort)
	}
	if c.Tunnels[0].ForwardingTimeout != "10s" {
		t.Errorf("want forwarding_timeout 10s, got %q", c.Tunnels[0].ForwardingTimeout)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	cases := []struct {
		name            string
		path            func(t *testing.T) string
		wantErrContains string
	}{
		{
			name: "missing file",
			path: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "not_exist.yml")
			},
			wantErrContains: "no such file",
		},
		{
			name: "broken yaml",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "broken.yml")
				if err := os.WriteFile(path, []byte("bastion_ssh_config: [\n"), 0600); err != nil {
					t.Fatalf("failed write config: %v", err)
				}
				return path
			},
			wantErrContains: "yaml",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Act
			_, err := LoadConfig(c.path(t))

			// Assert
			if err == nil {
				t.Fatal("want error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantErrContains) {
				t.Errorf("want error containing %q, got: %v", c.wantErrContains, err)
			}
		})
	}
}

func TestHostPort(t *testing.T) {
	cases := []struct {
		name string
		host string
		port int
		want string
	}{
		{name: "host and port", host: "bastion.example.com", port: 22, want: "bastion.example.com:22"},
		{name: "ip and port", host: "10.0.0.2", port: 53, want: "10.0.0.2:53"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hostport(c.host, c.port); got != c.want {
				t.Errorf("want %q, got %q", c.want, got)
			}
		})
	}
}

func TestLocalPort(t *testing.T) {
	// the local listener must stay on localhost, binding every interface would
	// expose the tunnel to the network.
	got := localport(8080)

	want := "localhost:8080"
	if got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}
