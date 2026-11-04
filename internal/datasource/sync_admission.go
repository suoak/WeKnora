package datasource

// SyncAdmissionError carries a fixed diagnostic category without source URLs,
// provider messages or credentials. It never grants admission or changes state.
type SyncAdmissionError struct {
	Reason          string
	Cause           error
	ProtectionState string
}

func (e *SyncAdmissionError) Error() string { return "sync_admission_rejected:" + e.Reason }
func (e *SyncAdmissionError) Unwrap() error { return e.Cause }
