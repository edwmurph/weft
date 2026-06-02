package version

var Version = "0.11.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
