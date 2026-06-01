package version

var Version = "8.1.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
