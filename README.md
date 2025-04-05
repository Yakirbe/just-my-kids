# Just My Kids

A WhatsApp bot that monitors group photos and only notifies you when your kids appear - filtering out all the other children. Perfect for busy parents who want to stay up-to-date with their children's school photos without scrolling through hundreds of images.

![Demo of WhatsApp Face Detection](output.gif)

## Why Use This?

- 👨‍👩‍👧‍👦 **Focus on what matters**: Get notified only when your children appear in photos
- 📲 **Save time**: No more scrolling through countless school and kindergarten group photos 
- 🔍 **Advanced detection**: Uses face recognition to identify your children with high accuracy
- 🔒 **Private and secure**: All processing happens locally on your machine
- ⚙️ **Highly customizable**: Configure thresholds and notification preferences

## Features

- 🔍 Real-time monitoring of WhatsApp groups
- 👥 Advanced face detection and recognition using multiple reference images
- 📱 Automatic WhatsApp notifications when known faces are detected
- 🖼️ Support for multiple image formats (JPG, PNG, HEIC)
- ⚙️ Highly configurable through a single JSON file
- 🎯 Enhanced accuracy with multiple reference photos per person

## Prerequisites

- Python 3.8+
- Go 1.16+
- WhatsApp account
- [face_recognition](https://github.com/ageitgey/face_recognition) library and its dependencies

## Installation

1. Clone the repository:
```bash
git clone https://github.com/yourusername/whatsapp-face-detection
cd whatsapp-face-detection
```

2. Install Python dependencies:
```bash
pip install -r requirements.txt
```

3. Install Go dependencies:
```bash
cd whatsapp-bridge
go mod download
```

## Setup

### 1. WhatsApp Client Setup

1. Start the WhatsApp bridge:
```bash
cd whatsapp-bridge
go run main.go
```

2. On first run, you'll see a QR code in the terminal. Scan it with WhatsApp to log in.

3. After logging in, the client will start outputting information about your chats. Look for lines like:
```
[GROUP] Name: Family Group (JID: 123456789012345678@g.us)
```
Take note of the group IDs (the numbers ending with @g.us) - you'll need these for configuration.

### 2. Getting Group IDs

There are several ways to find the group IDs needed for configuration:

1. **Using the list-groups command flag:**
   The simplest method is to use the `-list-groups` flag:
   ```bash
   cd whatsapp-bridge
   go run main.go -list-groups
   ```
   This will connect to WhatsApp, list all your groups with their IDs, and exit.

2. **From the connected client log:**
   When you run `go run main.go` and log in, look for these lines:
   ```
   [GROUPS] Found X groups:
   [GROUP] Name: Group Name (JID: 123456789012345678@g.us)
   ```

3. **From incoming messages:**
   In active groups, you'll see log entries like:
   ```
   [MESSAGE] Processing incoming message event
   ```
   followed by details including the chat JID.

### 3. Reference Images Setup

1. Create the main reference images directory:
```bash
mkdir reference_images
```

2. Create a subdirectory for each person:
```bash
cd reference_images
mkdir child1 child2 child3
```

3. Add multiple reference photos for each person:
```
reference_images/
  ├── child1/
  │   ├── front.jpg
  │   ├── side.jpg
  │   └── smiling.jpg
  ├── child2/
  │   ├── photo1.jpg
  │   ├── photo2.jpg
  │   └── photo3.jpg
  └── child3/
      ├── indoor.jpg
      └── outdoor.jpg
```

#### Reference Photo Best Practices:

- **Quantity matters**: 5-10 different photos per person will significantly improve accuracy
- **Include variety**: Different angles, expressions, lighting conditions, and backgrounds
- **Quality matters**: Use good quality, well-lit photos without motion blur
- **One face per image**: Ensure each reference photo contains exactly one face
- **Diverse environments**: Mix indoor and outdoor photos for better environmental adaptability
- **Expression range**: Include both neutral and expressive faces (smiling, serious, etc.)
- **Recent photos**: Use recent photos that match current appearance
- **Supported formats**: jpg, jpeg, png, heic

> **Pro tip**: The more reference images you provide and the more diverse they are, the better the system will perform at detecting faces in various conditions.

### 4. Configuration

The configuration file `config.json` controls all aspects of the system. Here's what each section means:

#### Input Groups (`input_groups`)
- List of WhatsApp group IDs to monitor for incoming images
- Format: `"XXXXXXXXXX@g.us"` or phone number-based group IDs
- Example: `"123456789012345678@g.us"`

#### Destinations (`destinations`)
Each person you want to monitor needs:
1. A directory of reference images in the `known_faces_dir` directory
2. A corresponding entry in the `destinations` configuration

Example configuration:
```json
"child1": {
    "name": "Child One",
    "group": "+1234567890"
}
```

- The key (`child1`) must match the directory name containing reference images
- `name`: Display name used in notifications
- `group`: WhatsApp group ID or phone number to send notifications to

#### Media Settings (`media`)
```json
"media": {
    "allowed_extensions": [".jpg", ".jpeg", ".png", ".heic", ".HEIC"],
    "store_path": "whatsapp-bridge/store/media"
}
```

- `allowed_extensions`: List of image file types to process
- `store_path`: Directory where incoming media files are temporarily stored

#### Face Detection Settings (`face_detection`)
```json
"face_detection": {
    "known_faces_dir": "reference_images",
    "min_matching_faces": 2,
    "confidence_threshold": 0.5,
    "model": "hog"
}
```

- `known_faces_dir`: Directory containing reference images
- `min_matching_faces`: How many reference photos need to match for positive identification
- `confidence_threshold`: Face distance threshold (0.0-1.0)
  - This is actually a distance metric: lower values = better matches
  - Recommended values: 0.4-0.6 (lower is stricter, higher is more permissive)
  - Lower values (e.g., 0.4) = fewer false positives but may miss some matches
  - Higher values (e.g., 0.6) = more matches but potentially more false positives
  - Recommended starting point: 0.5
- `model`: Face detection algorithm to use
  - `hog`: Faster processing, works well in most scenarios
  - `cnn`: More accurate but significantly slower, recommended for critical use cases or if running on powerful hardware

> **Note on Reference Images**: 
> The more reference images you provide per person, the better the system's accuracy. 
> Including 5-10 diverse high-quality images per person can significantly improve detection rates 
> and reduce false positives.

#### Debug Settings (Optional)
```json
"debug": {
    "enabled": false,
    "output_dir": "debug_output"
}
```

- `enabled`: Set to true to enable debug mode
- `output_dir`: Directory where debug information will be stored

#### Example Configuration

```json
{
    "input_groups": [
        "GROUP_ID1",
        "GROUP_ID2"
    ],
    "destinations": {
        "child1": {
            "name": "Child One",
            "group": "+MYSELF"
        },
        "child2": {
            "name": "Child Two",
            "group": "+MY_HUSBAND"
        }
    },
    "media": {
        "allowed_extensions": [".jpg", ".jpeg", ".png", ".heic", ".HEIC"],
        "store_path": "whatsapp-bridge/store/media"
    },
    "face_detection": {
        "known_faces_dir": "reference_images",
        "min_matching_faces": 2,
        "confidence_threshold": 0.5,
        "model": "hog"
    },
    "debug": {
        "enabled": false,
        "output_dir": "debug_output"
    }
}
```

### 5. Start the System

1. Start the WhatsApp bridge (if not already running):
```bash
cd whatsapp-bridge
go run main.go
```

You can also specify a custom port for the REST API (default is 8080):
```bash
go run main.go -port 8888
```

2. In a new terminal, start the face detection service:
```bash
python face_filter_service.py
```

## How It Works

1. The WhatsApp bridge monitors specified groups for new images
2. When an image is received, it's saved to the media directory
3. The face detection service monitors the media directory for new images
4. When a new image appears:
   - Faces are detected in the image
   - Each face is compared against all reference photos for each person
   - If enough reference photos match with high confidence, it's considered a match
   - A notification is sent via WhatsApp
   - The image is then deleted from the media directory

## Future Roadmap

- [ ] Video file support
  - [ ] Parse video frames at configurable intervals
  - [ ] Extract thumbnails from video messages
  - [ ] Support MP4, MOV, and other common formats
- [ ] Advanced notification options
  - [ ] Customizable notification templates
  - [ ] Delay/batch notifications
- [ ] Web interface for configuration

## Troubleshooting

### Face Detection Issues

1. If faces aren't being detected (too many false negatives):
- Check the image quality in reference photos
- Ensure faces are clearly visible and well-lit
- Try increasing the confidence_threshold in config.json (e.g., from 0.5 to 0.6)
- Switch to the more accurate "cnn" model by setting `"model": "cnn"` in the config (note this will be slower)
- Add more reference photos taken in similar lighting conditions to the problem images

2. If getting false positives (incorrect matches):
- Increase the min_matching_faces setting (e.g., from 2 to 3)
- Decrease the confidence_threshold (e.g., from 0.5 to 0.4) to require closer matches
- Add more diverse reference photos
- Ensure reference photos only contain one face
- Switch to the more accurate "cnn" model for better discrimination between similar faces

3. If getting false negatives (missed matches):
- Decrease the min_matching_faces setting
- Increase the confidence_threshold (e.g., from 0.5 to 0.6) to be more permissive
- Add more reference photos from different angles
- Include photos with similar lighting conditions
- Add photos with similar expressions
- If performance isn't a concern, set `"model": "cnn"` for more accurate face detection

4. If face detection is too slow:
- If using "cnn" model, switch to "hog" for faster processing
- Reduce the number of groups being monitored
- Consider running on a more powerful machine if using CNN mode
- Reduce image resolution if possible

> **Note**: The CNN model can be 2-5x slower than HOG but offers significantly better accuracy, especially for faces at odd angles or in challenging lighting conditions.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Acknowledgments

This project is based on the [WhatsApp MCP](https://github.com/lharries/whatsapp-mcp) by Luke Harries, which provides the underlying WhatsApp connectivity framework. We've extended the original project with face detection capabilities and notification systems.

## License

This project is licensed under the MIT License - see the LICENSE file for details.

Since this project builds upon [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp), please also respect the license terms of the original repository.
