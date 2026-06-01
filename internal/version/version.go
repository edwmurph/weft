package version

var Version = "0.1.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
