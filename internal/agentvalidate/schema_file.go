package agentvalidate

// CheckSchemaFile compiles an explicitly supplied, confined local schema using
// only corpus-local schema resources. No remote resolver is enabled.
func CheckSchemaFile(root, owner, reference string) error {
	c := &Corpus{Root: root}
	path, err := c.ResolveFile(owner, reference)
	if err != nil {
		return err
	}
	_, err = compileFileSchema(root, path)
	return err
}
