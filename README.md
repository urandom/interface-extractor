# interface-extractor

Utility for generating an interface for a given type used in a package. The utility will only include the methods used in the type when generating the interface.

## Usage

```bash
interface-extractor --type some.Type
```

The type flag requires a qualified identifier, even if the type belongs in the package where the `interface-extractor` is being run from.
