package e2e_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	// Enable verbose output to capture stdout from tests
	RunSpecs(t, "E2E Suite", Label("e2e"))
}
