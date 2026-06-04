package version

var Version = "0.15.2"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
