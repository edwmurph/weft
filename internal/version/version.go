package version

var Version = "0.13.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
