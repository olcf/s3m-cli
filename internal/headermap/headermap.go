package headermap

import "strings"

const DefaultEnclave = "open"

type Resolver interface {
	ServiceValues(enclave, service, header string) []string
	MethodValues(enclave, method, header string) []string
}

type StaticResolver struct {
	service map[string]map[string]map[string][]string
	method  map[string]map[string]map[string][]string
}

func DefaultResolver() Resolver {
	return defaultResolver
}

func NewStaticResolver(
	service map[string]map[string]map[string][]string,
	method map[string]map[string]map[string][]string,
) *StaticResolver {
	return &StaticResolver{
		service: service,
		method:  method,
	}
}

func (r *StaticResolver) ServiceValues(enclave, service, header string) []string {
	return lookup(r.service, enclave, service, header)
}

func (r *StaticResolver) MethodValues(enclave, method, header string) []string {
	return lookup(r.method, enclave, method, header)
}

func lookup(scope map[string]map[string]map[string][]string, enclave, target, header string) []string {
	if scope == nil {
		return nil
	}

	enclave = strings.ToLower(strings.TrimSpace(enclave))
	if enclave == "" {
		enclave = DefaultEnclave
	}

	enclaveMap := scope[enclave]
	if enclaveMap == nil && enclave != DefaultEnclave {
		enclaveMap = scope[DefaultEnclave]
	}

	if enclaveMap == nil {
		return nil
	}

	headers := enclaveMap[target]
	if headers == nil {
		return nil
	}

	return headers[header]
}

var defaultResolver = &StaticResolver{
	service: map[string]map[string]map[string][]string{
		"open": {
			"olcf.s3m.slurm.v0042.SlurmIndirect": {"olcf-resource": {"defiant", "quokka", "wombat"}},
			"olcf.s3m.slurm.v0043.SlurmIndirect": {"olcf-resource": {"defiant", "quokka", "wombat"}},
		},
	},
	method: map[string]map[string]map[string][]string{
		"open": {
			"olcf.s3m.slurm.v0042.SlurmIndirect.PostJobSubmit": {"x-s3m-withstorage": {"true", "false"}},
			"olcf.s3m.slurm.v0043.SlurmIndirect.PostJobSubmit": {"x-s3m-withstorage": {"true", "false"}},
		},
	},
}
