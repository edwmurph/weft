package version

var Version = "8.0.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
