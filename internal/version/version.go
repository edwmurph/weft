package version

var Version = "10.0.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
