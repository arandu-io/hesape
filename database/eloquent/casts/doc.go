// Package casts answers Illuminate\Database\Eloquent\Casts: the custom cast
// contract, the two encoders every cast shares, and the casts Illuminate ships.
//
// The files it answers to, in the clone at
// laravel_illuminate/Database/Eloquent/Casts:
//
//	ArrayObject.php              -> arrayobject.go
//	AsArrayObject.php            -> ascollection.go
//	AsBinary.php                 -> asbinary.go
//	AsCollection.php             -> ascollection.go
//	AsEncryptedArrayObject.php   -> asencryptedcollection.go
//	AsEncryptedCollection.php    -> asencryptedcollection.go
//	AsFluent.php                 -> asstringable.go
//	AsHtmlString.php             -> asstringable.go
//	AsStringable.php             -> asstringable.go
//	AsUri.php                    -> asstringable.go
//	Attribute.php                -> attribute.go
//	Json.php                     -> json.go
//
// # A static is a method on the zero value, not a package function
//
// Illuminate declares castUsing, of, using, uuid and ulid static, and resolves
// them through the cast string "AsCollection:App\Data". Go resolves nothing
// from a class name in a string, and two classes here spell the same static --
// AsCollection::of and AsBinary::of -- which a package function cannot hold
// twice. So each cast is a type, its statics are methods on it, and what the
// cast string carried is a field:
//
//	casts.AsCollection{}.Of(func(item any) (any, error) { … })
//	casts.AsBinary{}.UUID()
//
// The exception is Json, whose four statics have no per-class meaning at all:
// they are package functions, and the two encoders they replace are package
// variables behind a mutex rather than static properties.
//
// # What is skipped, and why (ADR 0044)
//
//   - AsEnumCollection and AsEnumArrayObject. Both take a class-string of a
//     PHP backed enum and call tryFrom on it. Go has no enum type and no way
//     to reach one from a name in a string: a Go "enum" is a named integer or
//     string, whose column already round-trips through the field's own type
//     with no cast at all. Motive (1), a PHP language feature Go does not have.
//   - AsCollection::using and AsEncryptedCollection::using. The argument is a
//     subclass of Illuminate's Collection, and Go has one generic
//     collections.Collection with no subclassing. What `using` adds over `of`
//     is that class alone, so keeping it would be a second spelling of Of
//     (RULE 9). Motive (1).
//   - Attribute's reflection half. The PHP finds an Attribute by declaring a
//     method whose return type is Attribute and reading that type back with
//     ReflectionMethod. Go cannot reflect a method's return type by name, so
//     the model registers the Attribute under its key --
//     concerns.HasAttributes.SetAttributeMutator -- and every has*Mutator
//     lookup reads that registry. Motive (1).
//   - The Crypt facade behind AsEncrypted*. There is no facade and no
//     container in this framework (ADR 0001, ADR 0002); the encrypter is a
//     field on the cast. Motive (2).
//   - Illuminate\Support\BinaryCodec, which AsBinary delegates to. It has one
//     caller in the whole framework, and a package with one caller is a name
//     to look up rather than a line to read; the two formats are decoded in
//     asbinary.go. Motive (3).
package casts
