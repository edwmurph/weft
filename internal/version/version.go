package version

var Version = "0.2.13"

var BuildChannel = "source"

func Label() string {
	return "Weft " + Version
}
