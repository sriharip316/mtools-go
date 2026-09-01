package hci

import (
	"testing"
	"time"
)

func TestString2DT(t *testing.T) {
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2015, 6, 13, 0, 0, 0, 0, time.UTC)
	dtb := New(start, end)

	lower := time.Date(2013, 5, 2, 16, 21, 58, 123000, time.UTC)

	tests := []struct {
		input      string
		lowerBound *time.Time
		expected   time.Time
	}{
		{"", nil, start},
		{"2013", nil, time.Date(2013, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"Aug 2011", nil, time.Date(2011, 8, 1, 0, 0, 0, 0, time.UTC)},
		{"29 Sep 1978", nil, time.Date(1978, 9, 29, 0, 0, 0, 0, time.UTC)},
		{"20 Mar", nil, time.Date(2015, 3, 20, 0, 0, 0, 0, time.UTC)},
		{"20 Aug", nil, time.Date(2014, 8, 20, 0, 0, 0, 0, time.UTC)},
		{"Sat", nil, time.Date(2015, 6, 13, 0, 0, 0, 0, time.UTC)},
		{"Wed", nil, time.Date(2015, 6, 10, 0, 0, 0, 0, time.UTC)},
		{"Sun", nil, time.Date(2015, 6, 7, 0, 0, 0, 0, time.UTC)},
		{"start", nil, time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"end", nil, time.Date(2015, 6, 13, 0, 0, 0, 0, time.UTC)},
		{"29 Sep 1978 13:06", nil, time.Date(1978, 9, 29, 13, 6, 0, 0, time.UTC)},
		{"13:06", nil, time.Date(2000, 1, 1, 13, 6, 0, 0, time.UTC)},
		{"13:06:15", nil, time.Date(2000, 1, 1, 13, 6, 15, 0, time.UTC)},
		{"Wed 13:06:15", nil, time.Date(2015, 6, 10, 13, 6, 15, 0, time.UTC)},
		{"2013 +1d", nil, time.Date(2013, 1, 2, 0, 0, 0, 0, time.UTC)},
		{"Sat -1d", nil, time.Date(2015, 6, 12, 0, 0, 0, 0, time.UTC)},
		{"Wed +4sec", nil, time.Date(2015, 6, 10, 0, 0, 4, 0, time.UTC)},
		{"Sun -26h", nil, time.Date(2015, 6, 5, 22, 0, 0, 0, time.UTC)},
		{"start +3h", nil, time.Date(2000, 1, 1, 3, 0, 0, 0, time.UTC)},
		{"-2d", nil, time.Date(2015, 6, 11, 0, 0, 0, 0, time.UTC)},
		{"2014-04-28T16:17:18.192Z", nil, time.Date(2014, 4, 28, 16, 17, 18, 192000000, time.UTC)},
		{"+3sec", &lower, time.Date(2013, 5, 2, 16, 22, 1, 123000, time.UTC)},
		{"+4min", &lower, time.Date(2013, 5, 2, 16, 25, 58, 123000, time.UTC)},
		{"-5hours", &lower, time.Date(2013, 5, 2, 11, 21, 58, 123000, time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			res, err := dtb.String2DT(tt.input, tt.lowerBound)
			if err != nil {
				t.Fatalf("unexpected error parsing %q: %v", tt.input, err)
			}
			if !res.Equal(tt.expected) {
				t.Errorf("parsing %q: expected %v, got %v", tt.input, tt.expected, res)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	start := time.Date(2012, 10, 14, 0, 0, 0, 0, time.UTC)
	end := time.Date(2013, 6, 2, 0, 0, 0, 0, time.UTC)
	dtb := New(start, end)

	tests := []struct {
		fromStr string
		toStr   string
		expFrom time.Time
		expTo   time.Time
	}{
		{"Feb 18 2013", "Feb 19 2013", time.Date(2013, 2, 18, 0, 0, 0, 0, time.UTC), time.Date(2013, 2, 19, 0, 0, 0, 0, time.UTC)},
		{"Sep 15 2012", "Dec 1 2012", time.Date(2012, 10, 14, 0, 0, 0, 0, time.UTC), time.Date(2012, 12, 1, 0, 0, 0, 0, time.UTC)},
		{"2013-01-15", "2016-03-02", time.Date(2013, 1, 15, 0, 0, 0, 0, time.UTC), time.Date(2013, 6, 2, 0, 0, 0, 0, time.UTC)},
		{"start", "end", time.Date(2012, 10, 14, 0, 0, 0, 0, time.UTC), time.Date(2013, 6, 2, 0, 0, 0, 0, time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.fromStr+" to "+tt.toStr, func(t *testing.T) {
			fromDT, toDT, err := dtb.Resolve(tt.fromStr, tt.toStr)
			if err != nil {
				t.Fatalf("unexpected error resolving %q to %q: %v", tt.fromStr, tt.toStr, err)
			}
			if !fromDT.Equal(tt.expFrom) {
				t.Errorf("expected from %v, got %v", tt.expFrom, fromDT)
			}
			if !toDT.Equal(tt.expTo) {
				t.Errorf("expected to %v, got %v", tt.expTo, toDT)
			}
		})
	}
}
