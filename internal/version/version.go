package version

var Version = "0.9.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
