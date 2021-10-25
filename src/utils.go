package main

func unique(data []string) []string {
	m := make(map[string]bool)
	for _, s := range data {
		m[s] = true
	}
	result := make([]string, 0, len(data))
	for s := range m {
		result = append(result, s)
	}
	return result
}
