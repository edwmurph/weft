package version

var Version = "0.21.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
