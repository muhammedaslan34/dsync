//go:build !windows

package main

import "errors"

// On Linux the firewall is opened by the package or install.sh (ufw or
// firewalld), which needs the user's password, so the app only helps on
// Windows.

func firewallStatus() FirewallStatus { return FirewallStatus{} }
func fixFirewall() error             { return errors.ErrUnsupported }
func makeNetworkPrivate() error      { return errors.ErrUnsupported }
