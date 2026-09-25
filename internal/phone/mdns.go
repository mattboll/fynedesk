package phone

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/grandcat/zeroconf"
)

// MDNS finds services with multicast DNS, as the phone announces them.
type MDNS struct{}

// mdnsRound is how long a search lasts: a new one hears again the services
// already heard, such as a phone that went away and came back.
const mdnsRound = time.Minute

// Browse calls found for each service of the kind announced, until ctx is
// done.
func (MDNS) Browse(ctx context.Context, service string, found func(Service)) error {
	for ctx.Err() == nil {
		if err := browseRound(ctx, service, found); err != nil {
			return err
		}
	}
	return nil
}

func browseRound(ctx context.Context, service string, found func(Service)) error {
	resolver, err := zeroconf.NewResolver(zeroconf.SelectIPTraffic(zeroconf.IPv4))
	if err != nil {
		return err
	}
	round, cancel := context.WithTimeout(ctx, mdnsRound)
	defer cancel()
	entries := make(chan *zeroconf.ServiceEntry)
	if err := resolver.Browse(round, service, "local.", entries); err != nil {
		return err
	}
	for e := range entries { // closed when the round ends
		if len(e.AddrIPv4) == 0 || e.Port == 0 {
			continue
		}
		found(Service{
			Instance: e.Instance,
			Addr:     net.JoinHostPort(e.AddrIPv4[0].String(), strconv.Itoa(e.Port)),
		})
	}
	return nil
}
