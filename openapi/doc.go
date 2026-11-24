// Package openapi is protokit's OpenAPI frontend: it loads a Swagger 2.0,
// OpenAPI 3.0 or OpenAPI 3.1 document (YAML or JSON, split across any number
// of files or URLs) and normalizes it into an apispec.Spec.
//
// The parser is self-contained (yaml.v3 only) and covers the parts of the
// specification that carry API meaning:
//
//   - every $ref form: local JSON pointers, relative files, absolute URLs,
//     with ~0/~1 and percent-escaping, and reference cycles
//   - schemas: type/format, 3.1 type arrays and "null", 3.0 nullable,
//     x-nullable, enum/const, allOf (merged, parents recorded), oneOf/anyOf
//     (unions, or a nullable wrapper when the only alternative is null),
//     discriminator, additionalProperties maps, readOnly/writeOnly,
//     deprecated, default/example, and every validation keyword
//   - inline object/enum/union schemas are lifted to deterministic named
//     types (ParentField, OperationRequest …) so nothing is anonymous
//   - paths: path-level + operation-level parameters (override by name+in),
//     2.0 body/formData parameters, 3.x requestBody, responses with headers,
//     media types, tags, security, servers, webhooks (3.1) and callbacks
//   - components/definitions: schemas, parameters, requestBodies, responses,
//     headers, securitySchemes/securityDefinitions
//   - root tags and x-tagGroups become apispec groups
package openapi
