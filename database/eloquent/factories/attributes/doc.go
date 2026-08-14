// Package attributes mirrors Illuminate\Database\Eloquent\Factories\Attributes.
//
// The files it answers to, in the clone at
// laravel_illuminate/database/Eloquent/Factories/Attributes:
//
//	UseModel.php
//
// Nothing is implemented here, and nothing will be. These are PHP 8 attributes:
// annotations a class carries, which the framework reads back by reflection to
// decide what to do with it.
//
// Go has struct tags and nothing else of the kind, and reading behaviour out of
// them is the mechanism this framework's thesis rejects -- what decides is the
// type, checked by the compiler. What an attribute configures in Illuminate is a
// field or an argument here.
package attributes
