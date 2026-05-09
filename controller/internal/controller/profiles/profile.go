package profiles

// Profile defines the Ziti attributes and extra services provisioned for a device type.
type Profile struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	Attributes    []string       `yaml:"attributes"`
	ExtraServices []ExtraService `yaml:"extra_services"`
}

// ExtraService is an additional Ziti service provisioned beyond the base set.
type ExtraService struct {
	Name string `yaml:"name"`
	Port int    `yaml:"port"`
}
