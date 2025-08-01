/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package container

import (
	"errors"
	"fmt"
	"net"

	"github.com/spf13/cobra"

	"github.com/containerd/go-cni"

	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/dnsutil"
	"github.com/containerd/nerdctl/v2/pkg/portutil"
	"github.com/containerd/nerdctl/v2/pkg/strutil"
)

func loadNetworkFlags(cmd *cobra.Command, globalOpts types.GlobalCommandOptions) (types.NetworkOptions, error) {
	netOpts := types.NetworkOptions{}

	// --net/--network=<net name> ...
	var netSlice = []string{}
	var networkSet = false
	if cmd.Flags().Lookup("network").Changed {
		network, err := cmd.Flags().GetStringSlice("network")
		if err != nil {
			return netOpts, err
		}
		netSlice = append(netSlice, network...)
		networkSet = true
	}
	if cmd.Flags().Lookup("net").Changed {
		net, err := cmd.Flags().GetStringSlice("net")
		if err != nil {
			return netOpts, err
		}
		netSlice = append(netSlice, net...)
		networkSet = true
	}

	if !networkSet {
		network, err := cmd.Flags().GetStringSlice("network")
		if err != nil {
			return netOpts, err
		}
		netSlice = append(netSlice, network...)
	}
	netOpts.NetworkSlice = strutil.DedupeStrSlice(netSlice)

	// --mac-address=<MAC>
	macAddress, err := cmd.Flags().GetString("mac-address")
	if err != nil {
		return netOpts, err
	}
	if macAddress != "" {
		if _, err := net.ParseMAC(macAddress); err != nil {
			return netOpts, err
		}
	}
	netOpts.MACAddress = macAddress

	// --ip=<container static IP>
	ipAddress, err := cmd.Flags().GetString("ip")
	if err != nil {
		return netOpts, err
	}
	netOpts.IPAddress = ipAddress

	// --ip6=<container static IP6>
	ip6Address, err := cmd.Flags().GetString("ip6")
	if err != nil {
		return netOpts, err
	}
	netOpts.IP6Address = ip6Address

	// -h/--hostname=<container hostname>
	hostName, err := cmd.Flags().GetString("hostname")
	if err != nil {
		return netOpts, err
	}
	netOpts.Hostname = hostName

	// --domainname=<container domainname>
	domainname, err := cmd.Flags().GetString("domainname")
	if err != nil {
		return netOpts, err
	}
	netOpts.Domainname = domainname

	// --dns=<DNS host> ...
	// Use command flags if set, otherwise use global config is set
	var dnsSlice []string
	if cmd.Flags().Changed("dns") {
		var err error
		dnsSlice, err = cmd.Flags().GetStringSlice("dns")
		if err != nil {
			return netOpts, err
		}
		if len(dnsSlice) == 0 {
			return netOpts, errors.New("--dns flag was specified but no DNS server was provided")
		}
		for _, dns := range dnsSlice {
			if _, err := dnsutil.ValidateIPAddress(dns); err != nil {
				return netOpts, fmt.Errorf("%w with --dns flag", err)
			}
		}
	} else {
		dnsSlice = globalOpts.DNS
	}
	netOpts.DNSServers = strutil.DedupeStrSlice(dnsSlice)

	// --dns-search=<domain name> ...
	// Use command flags if set, otherwise use global config is set
	var dnsSearchSlice []string
	if cmd.Flags().Changed("dns-search") {
		var err error
		dnsSearchSlice, err = cmd.Flags().GetStringSlice("dns-search")
		if err != nil {
			return netOpts, err
		}
	} else {
		dnsSearchSlice = globalOpts.DNSSearch
	}
	netOpts.DNSSearchDomains = strutil.DedupeStrSlice(dnsSearchSlice)

	// --dns-opt/--dns-option=<resolv.conf line> ...
	// Use command flags if set, otherwise use global config if set
	dnsOptions := []string{}

	// Check if either dns-opt or dns-option flags were set
	dnsOptChanged := cmd.Flags().Changed("dns-opt")
	dnsOptionChanged := cmd.Flags().Changed("dns-option")

	if dnsOptChanged || dnsOptionChanged {
		// Use command flags
		dnsOptFlags, err := cmd.Flags().GetStringSlice("dns-opt")
		if err != nil {
			return netOpts, err
		}
		dnsOptions = append(dnsOptions, dnsOptFlags...)

		dnsOptionFlags, err := cmd.Flags().GetStringSlice("dns-option")
		if err != nil {
			return netOpts, err
		}
		dnsOptions = append(dnsOptions, dnsOptionFlags...)
	} else {
		// Use global config defaults
		dnsOptions = append(dnsOptions, globalOpts.DNSOpts...)
	}

	netOpts.DNSResolvConfOptions = strutil.DedupeStrSlice(dnsOptions)

	// --add-host=<host:IP> ...
	addHostFlags, err := cmd.Flags().GetStringSlice("add-host")
	if err != nil {
		return netOpts, err
	}
	netOpts.AddHost = addHostFlags

	// --uts=<Unix Time Sharing namespace>
	utsNamespace, err := cmd.Flags().GetString("uts")
	if err != nil {
		return netOpts, err
	}
	netOpts.UTSNamespace = utsNamespace

	// -p/--publish=127.0.0.1:80:8080/tcp ...
	portSlice, err := cmd.Flags().GetStringSlice("publish")
	if err != nil {
		return netOpts, err
	}
	portSlice = strutil.DedupeStrSlice(portSlice)
	portMappings := []cni.PortMapping{}
	for _, p := range portSlice {
		pm, err := portutil.ParseFlagP(p)
		if err != nil {
			return netOpts, err
		}
		portMappings = append(portMappings, pm...)
	}
	netOpts.PortMappings = portMappings

	return netOpts, nil
}
