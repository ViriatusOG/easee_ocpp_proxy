package ocpp

// Typed OCPP 1.6 payloads for the messages the proxy actively answers or inspects.
// Everything else is relayed as raw JSON.

// --- Requests the proxy inspects ---

type BootNotificationReq struct {
	ChargePointVendor       string `json:"chargePointVendor"`
	ChargePointModel        string `json:"chargePointModel"`
	FirmwareVersion         string `json:"firmwareVersion,omitempty"`
	ChargePointSerialNumber string `json:"chargePointSerialNumber,omitempty"`
}

type StartTransactionReq struct {
	ConnectorID int    `json:"connectorId"`
	IDTag       string `json:"idTag"`
	MeterStart  int    `json:"meterStart"`
	Timestamp   string `json:"timestamp"`
}

type StopTransactionReq struct {
	TransactionID int    `json:"transactionId"`
	MeterStop     int    `json:"meterStop"`
	Reason        string `json:"reason,omitempty"`
	Timestamp     string `json:"timestamp"`
}

type StatusNotificationReq struct {
	ConnectorID int    `json:"connectorId"`
	ErrorCode   string `json:"errorCode"`
	Status      string `json:"status"`
}

type MeterValuesReq struct {
	ConnectorID int          `json:"connectorId"`
	MeterValue  []MeterValue `json:"meterValue"`
}

type MeterValue struct {
	Timestamp    string         `json:"timestamp"`
	SampledValue []SampledValue `json:"sampledValue"`
}

// SampledValue carries the explicit unit; the proxy must honour it and not assume
// Wh (Appendix A: Easee reports Energy.Active.Import.Register in kWh, power in W).
type SampledValue struct {
	Value     string `json:"value"`
	Measurand string `json:"measurand,omitempty"` // defaults to Energy.Active.Import.Register when absent
	Unit      string `json:"unit,omitempty"`
}

// --- Confirmations the proxy sends as local Central System ---

type IdTagInfo struct {
	Status string `json:"status"`
}

type BootNotificationConf struct {
	CurrentTime string `json:"currentTime"`
	Interval    int    `json:"interval"`
	Status      string `json:"status"`
}

type HeartbeatConf struct {
	CurrentTime string `json:"currentTime"`
}

type AuthorizeConf struct {
	IdTagInfo IdTagInfo `json:"idTagInfo"`
}

type StartTransactionConf struct {
	TransactionID int       `json:"transactionId"`
	IdTagInfo     IdTagInfo `json:"idTagInfo"`
}

type StopTransactionConf struct {
	IdTagInfo *IdTagInfo `json:"idTagInfo,omitempty"`
}

type DataTransferConf struct {
	Status string `json:"status"`
}

// --- Central-System-initiated requests the proxy sends ---

// RemoteStartTransactionReq asks a chargepoint to begin a transaction (used by the
// local auto-start, config LocalAutoStart).
type RemoteStartTransactionReq struct {
	ConnectorID int    `json:"connectorId,omitempty"`
	IDTag       string `json:"idTag"`
}

// RemoteStopTransactionReq asks a chargepoint to end a transaction (used to enforce a
// schedule window closing, FR-41).
type RemoteStopTransactionReq struct {
	TransactionID int `json:"transactionId"`
}
