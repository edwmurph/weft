package version

var Version = "0.19.0"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
