// Package config loads and saves settings in the user's config folder and
// lists the machine's addresses for the dashboard.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
)

// Config is what the app remembers between runs.
type Config struct {
	// Host is the IPv4 address to listen on. 127.0.0.1 means this machine only.
	Host  string `json:"host"`
	Port  int    `json:"port"`
	Token string `json:"token"`
}

const DefaultPort = 8787

// DefaultPath is <user config dir>/outlook-render/config.json
// (on Windows: %AppData%\outlook-render\config.json).
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "outlook-render", "config.json"), nil
}

// Load reads the config, creating it with a fresh random token if missing.
func Load(path string) (*Config, error) {
	c := &Config{Host: "127.0.0.1", Port: DefaultPort}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		c.Token = NewToken()
		return c, Save(path, c)
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if c.Token == "" {
		c.Token = NewToken()
		if err := Save(path, c); err != nil {
			return nil, err
		}
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	return c, c.Validate()
}

// Save writes the config, readable only by the current user where the OS supports it.
func Save(path string, c *Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Validate checks the host is an IPv4 address and the port is usable.
func (c *Config) Validate() error {
	if ip := net.ParseIP(c.Host); ip == nil || ip.To4() == nil {
		return fmt.Errorf("host must be an IPv4 address, got %q", c.Host)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be 1-65535, got %d", c.Port)
	}
	if len(c.Token) < 16 {
		return fmt.Errorf("token must be at least 16 characters")
	}
	return nil
}

// NewToken returns 32 random bytes, base64url-encoded.
func NewToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// Address is a local IPv4 address the server could listen on.
type Address struct {
	IP        string `json:"ip"`
	Interface string `json:"interface"`
}

// LocalAddresses lists this machine's IPv4 addresses, loopback first.
func LocalAddresses() []Address {
	out := []Address{{IP: "127.0.0.1", Interface: "this machine only"}}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	var rest []Address
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil || ipnet.IP.IsLinkLocalUnicast() {
				continue
			}
			rest = append(rest, Address{IP: ipnet.IP.String(), Interface: iface.Name})
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].Interface < rest[j].Interface })
	return append(out, rest...)
}
