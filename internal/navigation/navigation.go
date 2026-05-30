package navigation

func MoveIndex(current int, count int, delta int) int {
	if count <= 0 {
		return 0
	}
	next := current + delta
	if next < 0 {
		return 0
	}
	if next >= count {
		return count - 1
	}
	return next
}

func IndexByID(ids []string, id string) int {
	for index, candidate := range ids {
		if candidate == id {
			return index
		}
	}
	return 0
}
