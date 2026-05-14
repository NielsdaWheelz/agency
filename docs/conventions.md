# Conventions

## Scope

This document covers small implementation conventions that do not belong to a larger topic.

## Named Constants

- Extract a value into a named constant when the name conveys information beyond what the usage site already says.
- Keep a value inline when it is inherently part of the expression.

## Timestamps

- Persist instants in UTC RFC3339 format.
- Keep time formatting at the boundary that writes the value.

## Lossy Conversions

- When converting from a rich type to a lossy or primitive representation, perform the conversion as late as possible.
- Do not pre-compute lossy forms into variables unless the name carries real meaning.

## Permissions

- Use named constants for file and directory permissions when the permission level matters to the contract.
