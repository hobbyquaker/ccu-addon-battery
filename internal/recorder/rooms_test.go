package recorder

import (
	"reflect"
	"testing"

	"github.com/hobbyquaker/ccu-addon-kit/go/ccusource"
	"github.com/hobbyquaker/ccu-addon-kit/go/litesource"
)

// The rooms of a device come from its channels: openccu-lite's metadata puts them there and
// leaves the device object without one, a classic CCU's ReGa likewise (ccusource adds the union
// to the device as well). Each room once, in channel order, in the asked language.
func TestDeviceRooms(t *testing.T) {
	rooms := litesource.MetaEnum{Tree: []litesource.MetaNode{
		{ID: "kueche", Name: litesource.MetaText{"de": "Küche", "en": "Kitchen"}},
		{ID: "flur", Name: litesource.MetaText{"de": "Flur", "en": "Hall"}},
		{ID: "bad", Name: litesource.MetaText{"de": "Bad", "en": "Bathroom"}},
	}}
	lite := litesource.NewNames(&litesource.MetaSnapshot{Revision: 1, Enums: map[string]litesource.MetaEnum{"room": rooms}, Objects: map[string]litesource.MetaObject{
		"HmIP-RF.0001":    {Name: "Taster"},
		"HmIP-RF.0001:0":  {Name: "Taster:0"},
		"HmIP-RF.0001:10": {Name: "Taster:10", Enums: []string{"room/bad", "function/light"}},
		"HmIP-RF.0001:2":  {Name: "Taster:2", Enums: []string{"room/flur", "room/kueche"}},
		"HmIP-RF.0001:1":  {Name: "Taster:1", Enums: []string{"room/kueche"}},
		"HmIP-RF.0002":    {Name: "Raumlos"},
		"HmIP-RF.0002:1":  {Name: "Raumlos:1"},
		"HmIP-RF.0003":    {Name: "Am Gerät", Enums: []string{"room/flur"}},
		"HmIP-RF.0003:1":  {Name: "Am Gerät:1", Enums: []string{"room/flur", "room/bad"}},
		// another device whose address starts the same must not leak in
		"HmIP-RF.00011:1":  {Name: "Andere", Enums: []string{"room/bad"}},
		"BidCos-RF.0001:1": {Name: "Andere Schnittstelle", Enums: []string{"room/bad"}},
	}})
	classic := litesource.NewNames(ccusource.ParseNames("D\tBidCos-RF\tJEQ01\tFenster\n" +
		"C\tJEQ01:0\tFenster:0\t\n" +
		"C\tJEQ01:1\tFenster:1\t1230|1231|\n" +
		"C\tJEQ01:2\tFenster:2\t1231|\n" +
		"D\tBidCos-RF\tJEQ02\tOhne\nC\tJEQ02:1\tOhne:1\t\n" +
		"R\t1230\tWohnzimmer\nR\t1231\tKüche\n"))

	for _, tc := range []struct {
		name    string
		n       *litesource.Names
		iface   string
		address string
		lang    string
		want    []string
	}{
		{"lite channels, de", lite, "HmIP-RF", "0001", "de", []string{"Küche", "Flur", "Bad"}},
		{"lite channels, en", lite, "HmIP-RF", "0001", "en", []string{"Kitchen", "Hall", "Bathroom"}},
		{"lite no rooms", lite, "HmIP-RF", "0002", "de", []string{}},
		{"lite device and channel", lite, "HmIP-RF", "0003", "de", []string{"Flur", "Bad"}},
		{"lite unknown device", lite, "HmIP-RF", "0009", "de", []string{}},
		{"classic union deduplicated", classic, "BidCos-RF", "JEQ01", "de", []string{"Wohnzimmer", "Küche"}},
		{"classic no rooms", classic, "BidCos-RF", "JEQ02", "de", []string{}},
		{"no names", nil, "HmIP-RF", "0001", "de", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := deviceRooms(tc.n, channelIndex(tc.n), tc.iface, tc.address, tc.lang)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
