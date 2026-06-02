package version

var Version = "0.4.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
