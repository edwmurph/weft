package version

var Version = "0.4.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
