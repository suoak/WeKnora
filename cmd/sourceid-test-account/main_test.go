package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceIDAccountPrepCLIInputBoundary(t *testing.T) {
	if os.Getenv("ACCOUNT_PREP_TEST_CHILD") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{os.Args[0]}, os.Args[i+1:]...)
				break
			}
		}
		main()
		return
	}
	for _, tc := range []struct {
		args            []string
		input, category string
	}{
		{[]string{
			"--protected-json-stdin",
		}, "{\"email\":\"synthetic@example.test\",\"database_dsn\":\"private-db\"," +
			"\"client_secret\":\"synthetic-private\"}", "protected_input_invalid"},
		{
			[]string{
				"--protected-json-stdin",
			},
			"{\"email\":\"synthetic@example.test\"," +
				"\"database_dsn\":\"private-db\"}{}",
			"protected_input_invalid",
		},
		{nil, "synthetic@example.test", "protected_database_configuration_required"},
		{[]string{"--tenant=synthetic-private"}, "", ""},
	} {
		cmd := exec.Command(
			os.Args[0],
			append([]string{"-test.run=^TestSourceIDAccountPrepCLIInputBoundary$", "--"}, tc.args...)...)
		cmd.Env = append(os.Environ(), "ACCOUNT_PREP_TEST_CHILD=1", "SOURCEID_TEST_ACCOUNT_DSN=")
		cmd.Stdin = strings.NewReader(tc.input)
		output, err := cmd.CombinedOutput()
		require.Error(t, err)
		require.NotContains(t, string(output), "synthetic-private")
		require.NotContains(t, string(output), "synthetic@example.test")
		require.NotContains(t, string(output), "private-db")
		if tc.category != "" {
			require.Contains(t, string(output), tc.category)
		}
	}
}
