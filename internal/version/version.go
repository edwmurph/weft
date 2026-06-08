package version

var Version = "0.20.4"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
