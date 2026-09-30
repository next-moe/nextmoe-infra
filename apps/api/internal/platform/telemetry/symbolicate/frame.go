package symbolicate

const (
	KindException = "exception"
	KindContract  = "contract"
	KindServer    = "server"
	KindJava      = "java"
	KindNative    = "native"
	KindANR       = "anr"
)

type Frame struct {
	Function string `json:"function,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Module   string `json:"module,omitempty"`
	InApp    bool   `json:"in_app,omitempty"`
	Raw      string `json:"raw,omitempty"`
	Path     string `json:"-"`
	PC       uint64 `json:"-"`
	BuildID  string `json:"-"`
}

type LLVMSymbol struct {
	FunctionName string
	FileName     string
	Line         int
	Column       int
}

type LLVMAddress struct {
	Address    uint64
	ModuleName string
	Symbols    []LLVMSymbol
}
