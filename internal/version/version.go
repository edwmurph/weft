package version

var Version = "0.20.8"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
