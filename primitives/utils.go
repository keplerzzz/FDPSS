package primitives

func IntIndexOf(list []int, val int) int {
	for i, v := range list {
		if v == val {
			return i
		}
	}
	return -1
}
