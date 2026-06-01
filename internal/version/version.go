package version

var Version = "9.0.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
