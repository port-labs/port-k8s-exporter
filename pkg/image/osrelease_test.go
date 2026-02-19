package image

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseOsRelease(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *OsInfo
	}{
		{
			name: "Debian",
			input: `PRETTY_NAME="Debian GNU/Linux 12 (bookworm)"
NAME="Debian GNU/Linux"
VERSION_ID="12"
VERSION="12 (bookworm)"
VERSION_CODENAME=bookworm
ID=debian
HOME_URL="https://www.debian.org/"
SUPPORT_URL="https://www.debian.org/support"
BUG_REPORT_URL="https://bugs.debian.org/"`,
			expected: &OsInfo{
				ID:         "debian",
				VersionID:  "12",
				PrettyName: "Debian GNU/Linux 12 (bookworm)",
				HomeURL:    "https://www.debian.org/",
			},
		},
		{
			name: "Alpine",
			input: `NAME="Alpine Linux"
ID=alpine
VERSION_ID=3.19.1
PRETTY_NAME="Alpine Linux v3.19"
HOME_URL="https://alpinelinux.org/"
BUG_REPORT_URL="https://gitlab.alpinelinux.org/alpine/aports/-/issues"`,
			expected: &OsInfo{
				ID:         "alpine",
				VersionID:  "3.19.1",
				PrettyName: "Alpine Linux v3.19",
				HomeURL:    "https://alpinelinux.org/",
			},
		},
		{
			name: "Ubuntu",
			input: `PRETTY_NAME="Ubuntu 22.04.3 LTS"
NAME="Ubuntu"
VERSION_ID="22.04"
VERSION="22.04.3 LTS (Jammy Jellyfish)"
VERSION_CODENAME=jammy
ID=ubuntu
ID_LIKE=debian
HOME_URL="https://www.ubuntu.com/"
SUPPORT_URL="https://help.ubuntu.com/"
BUG_REPORT_URL="https://bugs.launchpad.net/ubuntu/"`,
			expected: &OsInfo{
				ID:         "ubuntu",
				VersionID:  "22.04",
				PrettyName: "Ubuntu 22.04.3 LTS",
				HomeURL:    "https://www.ubuntu.com/",
			},
		},
		{
			name: "RHEL",
			input: `NAME="Red Hat Enterprise Linux"
VERSION="9.3 (Plow)"
ID="rhel"
ID_LIKE="fedora"
VERSION_ID="9.3"
PRETTY_NAME="Red Hat Enterprise Linux 9.3 (Plow)"
HOME_URL="https://www.redhat.com/"`,
			expected: &OsInfo{
				ID:         "rhel",
				VersionID:  "9.3",
				PrettyName: "Red Hat Enterprise Linux 9.3 (Plow)",
				HomeURL:    "https://www.redhat.com/",
			},
		},
		{
			name:  "empty input",
			input: "",
			expected: &OsInfo{
				ID:         "",
				VersionID:  "",
				PrettyName: "",
				HomeURL:    "",
			},
		},
		{
			name: "comments and blank lines",
			input: `# This is a comment

ID=debian
# Another comment
VERSION_ID="12"

`,
			expected: &OsInfo{
				ID:        "debian",
				VersionID: "12",
			},
		},
		{
			name: "unknown keys ignored",
			input: `ID=alpine
VERSION_ID=3.19
CUSTOM_KEY="custom_value"
ANOTHER_KEY=something`,
			expected: &OsInfo{
				ID:        "alpine",
				VersionID: "3.19",
			},
		},
		{
			name: "single-quoted values",
			input: `ID='centos'
VERSION_ID='8'
PRETTY_NAME='CentOS Stream 8'
HOME_URL='https://centos.org/'`,
			expected: &OsInfo{
				ID:         "centos",
				VersionID:  "8",
				PrettyName: "CentOS Stream 8",
				HomeURL:    "https://centos.org/",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseOsRelease(strings.NewReader(tt.input))
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
