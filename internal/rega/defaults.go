package rega

import "strings"

// defaultNames are the rooms and functions a CCU ships with. ReGa keeps them under a key
// ("roomKitchen", "funcLight") and the WebUI translates the key in the browser, with these
// texts (its translate.lang.extension.js, de and en); a room the user renamed or made is plain
// text. Read from OpenCCU 3.89.8.
var defaultNames = map[string][2]string{ // key: de, en
	"roomLivingRoom":     {"Wohnzimmer", "Living room"},
	"roomKitchen":        {"Küche", "Kitchen"},
	"roomBedroom":        {"Schlafzimmer", "Bed room"},
	"roomChildrensRoom1": {"Kinderzimmer 1", "Children's room 1"},
	"roomChildrensRoom2": {"Kinderzimmer 2", "Children's room 2"},
	"roomOffice":         {"Büro", "Home office"},
	"roomBathroom":       {"Badezimmer", "Bathroom"},
	"roomGarage":         {"Garage", "Garage"},
	"roomHWR":            {"Hauswirtschaftsraum", "Utility room"},
	"roomGarden":         {"Garten", "Garden"},
	"roomTerrace":        {"Terrasse", "Terrace"},
	"funcLight":          {"Licht", "Light"},
	"funcHeating":        {"Heizung", "Heating"},
	"funcClimateControl": {"Klima", "Climatic conditions"},
	"funcWeather":        {"Wetter", "Weather"},
	"funcEnvironment":    {"Umwelt", "Environment"},
	"funcSecurity":       {"Sicherheit", "Security"},
	"funcLock":           {"Verschluss", "Lock"},
	"funcButton":         {"Taster", "Button"},
	"funcCentral":        {"Zentrale", "Central control unit"},
	"funcEnergy":         {"Energiemanagement", "Energy management"},
}

// DefaultName answers the text of a room or function the CCU shipped with, in lang (German for
// "de…", English otherwise), as the WebUI shows it; any other name - everything the user typed
// - comes back as it is.
func DefaultName(name, lang string) string {
	t, ok := defaultNames[name]
	if !ok {
		return name
	}
	if strings.HasPrefix(strings.ToLower(lang), "de") {
		return t[0]
	}
	return t[1]
}
