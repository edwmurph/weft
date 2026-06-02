package version

var Version = "0.3.3"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
