package version

var Version = "9.0.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
