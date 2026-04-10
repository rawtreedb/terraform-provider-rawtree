# Contributing to the Rawtree Terraform Provider

Thank you for your interest in contributing! This document provides guidelines for contributing to this project.

## Getting Started

### Prerequisites

- [Go](https://golang.org/doc/install) >= 1.22
- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- AWS credentials for running acceptance tests
- A Rawtree account for running acceptance tests

### Setting Up Your Development Environment

1. Clone the repository:

   ```sh
   git clone https://github.com/rawtreedb/terraform-provider-rawtree.git
   cd terraform-provider-rawtree
   ```

2. Build the provider:

   ```sh
   make build
   ```

3. Run unit tests:

   ```sh
   make test
   ```

## Making Changes

### Code Structure

```
internal/
├── client/              # Shared Rawtree API client
├── provider/            # Provider configuration and setup
└── resources/
    └── s3_ingestion/    # S3 ingestion resource
        ├── resource.go  # CRUD lifecycle
        ├── schema.go    # Resource schema
        ├── models.go    # State models
        ├── iam.go       # IAM role management
        ├── glue.go      # Glue job management
        ├── lambda.go    # Lambda function management
        ├── eventbridge.go # EventBridge setup
        └── scripts/     # Embedded Python scripts
```

### Adding a New Resource

1. Create a new directory under `internal/resources/`
2. Implement the `resource.Resource` interface using `terraform-plugin-framework`
3. Register the resource in `internal/provider/provider.go`
4. Add documentation in `templates/` and examples in `examples/`
5. Run `make generate` to update docs

### Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use meaningful variable and function names
- Keep functions focused and reasonably sized

## Pull Request Process

1. Fork the repository and create a feature branch
2. Make your changes with clear, descriptive commits
3. Ensure all tests pass: `make test`
4. Ensure code is formatted: `make fmt`
5. Update documentation if needed
6. Open a pull request with a clear description of the change

## Reporting Issues

- Use GitHub Issues to report bugs
- Include Terraform version, provider version, and relevant configuration
- Include the full error message and debug output if applicable

## License

By contributing, you agree that your contributions will be licensed under the Mozilla Public License v2.0.
