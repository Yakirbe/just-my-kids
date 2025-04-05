# Reference Images Directory

This directory is where you should place your reference images for face detection.

## Directory Structure

```
reference_images/
  ├── person1/             # Directory named after the person (use the same name as in config.json)
  │   ├── photo1.jpg       # Multiple reference photos of the same person
  │   ├── photo2.jpg
  │   └── photo3.jpg
  ├── person2/
  │   ├── photo1.jpg
  │   ├── photo2.jpg
  │   └── photo3.jpg
  ...
```

## Best Practices

1. Create a **separate directory for each person** you want to detect
2. The directory name must **match the key in your config.json** `destinations` section
3. Include **multiple (5-10) photos** of each person for best results
4. Use **diverse photos** with different angles, expressions, and lighting
5. Ensure each photo contains **only one face**
6. Use **good quality, well-lit** photos

## Example

If your config.json has:

```json
"destinations": {
    "john": {
        "name": "John Smith",
        "group": "123456789@g.us"
    }
}
```

Then your directory structure should be:

```
reference_images/
  └── john/             # Name matches the "john" key in config.json
      ├── front.jpg
      ├── side.jpg
      ├── smiling.jpg
      └── outdoor.jpg
```

This `.example` directory is just a placeholder to show the structure - you should create your own directories with actual reference photos. 