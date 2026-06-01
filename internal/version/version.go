package version

var Version = "0.2.3"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
