package version

var Version = "0.2.8"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
