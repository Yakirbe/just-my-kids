import face_recognition
import numpy as np
import os
import json
import time
import requests
from watchdog.observers import Observer
from watchdog.events import FileSystemEventHandler
from typing import Optional, List, Dict, Any
import cv2
from datetime import datetime
import humanize  # For human readable file sizes

class Config:
    def __init__(self, config_file="config.json"):
        self.config_file = config_file
        self.config = self.load_config()
        
        # Create debug directory if debug mode is enabled
        if self.get_debug_mode():
            os.makedirs(self.get_debug_dir(), exist_ok=True)

    def load_config(self):
        if os.path.exists(self.config_file):
            with open(self.config_file, 'r') as f:
                return json.load(f)
        raise FileNotFoundError(f"Config file {self.config_file} not found")

    def get_debug_mode(self) -> bool:
        debug = self.config.get("debug", {}).get("enabled", False)
        return debug

    def get_debug_dir(self) -> str:
        return self.config.get("debug", {}).get("output_dir", "debug_output")

    def get_allowed_extensions(self):
        return self.config["media"]["allowed_extensions"]

    def get_media_store_path(self):
        return self.config["media"]["store_path"]

    def get_known_faces_dir(self):
        return self.config["face_detection"]["known_faces_dir"]

    def get_confidence_threshold(self):
        return self.config["face_detection"]["confidence_threshold"]
    
    def get_face_detection_model(self):
        return self.config["face_detection"].get("model", "hog")

    def get_min_matching_faces(self):
        return self.config["face_detection"].get("min_matching_faces", 2)

    def get_destination_info(self, kid_name):
        return self.config["destinations"].get(kid_name)

def main():
    config = Config()
    
    # Create and clean media directory
    media_dir = config.get_media_store_path()
    os.makedirs(media_dir, exist_ok=True)
    
    print("\nCleaning up media directory...")
    for filename in os.listdir(media_dir):
        try:
            file_path = os.path.join(media_dir, filename)
            if os.path.isfile(file_path):
                file_info = get_file_info(file_path)
                os.remove(file_path)
                print(f"[DELETE] Removed {file_info}")
                print(f"[DELETE] Reason: Startup cleanup of media directory")
        except OSError as e:
            print(f"[ERROR] Failed to delete {filename}: {e}")
    
    # Start watching for new images
    observer = Observer()
    event_handler = ImageHandler(config)
    observer.schedule(event_handler, media_dir, recursive=False)
    observer.start()

    print(f"\nWatching for new images in {media_dir}...")
    
    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        observer.stop()
    observer.join()

class FaceCV:
    def __init__(self, config):
        self.config = config
        self.model = config.get_face_detection_model()
        print(f"Using face detection model: {self.model}")

    def process_image(self, image_path: str) -> List[Dict[str, Any]]:
        """Process an image and return detected faces with their encodings"""
        image = face_recognition.load_image_file(image_path)
        face_locations = face_recognition.face_locations(image, model=self.model)
        face_encodings = face_recognition.face_encodings(image, face_locations)
        
        results = []
        for location, encoding in zip(face_locations, face_encodings):
            top, right, bottom, left = location
            face_image = image[top:bottom, left:right]
            results.append({
                'encoding': encoding,
                'location': location,
                'face_image': face_image
            })
        return results

    def compare_faces(self, known_encoding: np.ndarray, face_encoding: np.ndarray) -> float:
        """Compare two face encodings using L2 distance"""
        return np.linalg.norm(known_encoding - face_encoding)

class FaceFilter:
    def __init__(self, config):
        self.config = config
        self.known_faces_dir = config.get_known_faces_dir()
        self.known_faces = {}  # {name: [encodings]}
        self.face_cv = FaceCV(config)
        self.load_known_faces()

    def load_known_faces(self):
        """Load known faces from the data directory"""
        print("Loading known faces...")
        self.known_faces = {}

        for person_dir in os.listdir(self.known_faces_dir):
            person_path = os.path.join(self.known_faces_dir, person_dir)
            if not os.path.isdir(person_path):
                continue

            print(f"\nLoading reference images for {person_dir}...")
            person_encodings = []

            for filename in os.listdir(person_path):
                if filename.lower().endswith(tuple(self.config.get_allowed_extensions())):
                    image_path = os.path.join(person_path, filename)
                    try:
                        # Load and get face encoding
                        faces = self.face_cv.process_image(image_path)
                        if faces:
                            person_encodings.append(faces[0]['encoding'])
                            print(f"  Loaded face from {filename}")
                    except Exception as e:
                        print(f"  Error processing {filename}: {e}")

            if person_encodings:
                self.known_faces[person_dir] = person_encodings
                print(f"  Loaded {len(person_encodings)} faces for {person_dir}")
            else:
                print(f"  No valid faces found for {person_dir}")

        print(f"\nLoaded faces for {len(self.known_faces)} people")

    def process_image(self, image_path: str) -> List[Dict[str, Any]]:
        """Process image and find matches with known faces"""
        try:
            # Detect faces in image
            faces = self.face_cv.process_image(image_path)
            if not faces:
                print("No faces found in image")
                return []

            results = []
            for face in faces:
                # Compare with all known faces
                matches_by_person = {}
                
                for person_name, reference_encodings in self.known_faces.items():
                    # Track matches for each reference image
                    matched_references = []
                    
                    for ref_enc in reference_encodings:
                        distance = self.face_cv.compare_faces(ref_enc, face['encoding'])
                        # If distance is below threshold, this reference image is a match
                        if distance < self.config.get_confidence_threshold():
                            matched_references.append({
                                'distance': distance,
                                'encoding': ref_enc
                            })
                    
                    # If we have enough matching reference images, consider this a match
                    min_matches_required = self.config.get_min_matching_faces()
                    if len(matched_references) >= min_matches_required:
                        # Sort by distance and get the best distance
                        matched_references.sort(key=lambda x: x['distance'])
                        best_distance = matched_references[0]['distance']
                        
                        matches_by_person[person_name] = {
                            'name': person_name,
                            'distance': best_distance,
                            'matched_count': len(matched_references),
                            'total_references': len(reference_encodings)
                        }
                
                # If no matches found, continue
                if not matches_by_person:
                    continue
                    
                # Sort by distance and get best match
                best_matches = sorted(matches_by_person.values(), key=lambda x: x['distance'])
                best_match = best_matches[0]
                
                print(f"\nBest match: {best_match['name']} with distance {best_match['distance']:.2f}")
                print(f"Matched {best_match['matched_count']}/{best_match['total_references']} reference images")
                
                # Save debug image if enabled
                if self.config.get_debug_mode():
                    debug_path = os.path.join(
                        self.config.get_debug_dir(),
                        f"match_{best_match['name']}_{datetime.now().strftime('%Y%m%d_%H%M%S')}.jpg"
                    )
                    cv2.imwrite(debug_path, cv2.cvtColor(face['face_image'], cv2.COLOR_RGB2BGR))
                
                results.append({
                    'face': face,
                    'match': best_match
                })

            return results

        except Exception as e:
            print(f"Error processing image: {e}")
            return []

class ImageHandler(FileSystemEventHandler):
    def __init__(self, config):
        self.config = config
        self.face_filter = FaceFilter(config)
        self.processed_files = set()  # Simple set to track already processed files

    def validate_image(self, file_path: str) -> bool:
        """Check if image exists and has valid extension"""
        # Check if file exists
        if not os.path.exists(file_path):
            print(f"[ERROR] Image no longer exists: {file_path}")
            return False
            
        # Check if file has valid extension
        if not file_path.lower().endswith(tuple(self.config.get_allowed_extensions())):
            print(f"[ERROR] File has invalid extension: {file_path}")
            return False
            
        return True
    
    def detect_faces(self, image_path: str) -> List[Dict[str, Any]]:
        """Detect and match faces in an image"""
        print(f"Detecting faces in {os.path.basename(image_path)}...")
        
        # Check again if image exists before processing
        if not os.path.exists(image_path):
            print(f"[ERROR] Image disappeared before face detection: {image_path}")
            return []
            
        matches = self.face_filter.process_image(image_path)
        
        if not matches:
            print(f"No matching faces found in {os.path.basename(image_path)}")
        else:
            print(f"Found {len(matches)} matching faces in {os.path.basename(image_path)}")
            
        return matches
    
    def send_notification(self, person_name: str, image_path: str) -> bool:
        """Send a single WhatsApp notification"""
        # Check again if image exists before sending
        if not os.path.exists(image_path):
            print(f"[ERROR] Image disappeared before sending notification: {image_path}")
            return False

        dest_info = self.config.get_destination_info(person_name)
        if not dest_info:
            print(f"No destination info found for {person_name}")
            return False

        try:
            payload = {
                "phone": dest_info["group"],
                "message": "",
                "media_url": image_path,
                "media_type": "image",
                "caption": dest_info["name"]
            }
            
            # Verify image exists right before sending
            if not os.path.exists(image_path):
                print(f"Error: Image {image_path} was deleted before sending")
                return False
                
            response = requests.post(
                "http://localhost:8080/api/send", 
                json=payload, 
                timeout=30
            )
            response.raise_for_status()
            print(f"Notification sent successfully for {person_name}")
            return True
            
        except Exception as e:
            print(f"Error sending notification for {person_name}: {e}")
            return False
    
    def send_all_notifications(self, matches: List[Dict], image_path: str) -> bool:
        """Send notifications for all matched faces"""
        if not matches:
            print("No matches to send notifications for")
            return True
            
        print(f"Sending {len(matches)} notifications...")
        all_succeeded = True
        
        for i, match in enumerate(matches):
            person_name = match['match']['name']
            print(f"Sending notification {i+1}/{len(matches)} for {person_name}...")
            
            # Check if image still exists
            if not os.path.exists(image_path):
                print(f"[ERROR] Image disappeared during notification process: {image_path}")
                return False
                
            success = self.send_notification(person_name, image_path)
            
            if not success:
                print(f"⚠️ Failed to send notification for {person_name}")
                all_succeeded = False
        
        if all_succeeded:
            print(f"✅ All {len(matches)} notifications were sent successfully")
        else:
            print(f"❌ Some notifications failed to send")
            
        return all_succeeded
    
    def delete_image(self, image_path: str) -> bool:
        """Delete an image file"""
        if not os.path.exists(image_path):
            print(f"[SKIP] Image already deleted: {image_path}")
            return True
            
        try:
            file_info = get_file_info(image_path)
            os.remove(image_path)
            print(f"[DELETE] Removed {file_info}")
            return True
        except OSError as e:
            print(f"[ERROR] Failed to delete file {image_path}: {e}")
            return False

    def process_file(self, file_path: str) -> None:
        """Process a single file through all steps"""
        # Skip if already processed
        if file_path in self.processed_files:
            return
            
        # Mark as processed
        self.processed_files.add(file_path)
        
        print(f"\n=== Processing: {get_file_info(file_path)} ===")
        
        # Step 1: Validate the image
        print("Step 1: Validating image...")
        if not self.validate_image(file_path):
            return
            
        # Step 2: Detect faces
        print("Step 2: Detecting faces...")
        matches = self.detect_faces(file_path)
        
        # Step 3: Send notifications
        print("Step 3: Sending notifications...")
        success = self.send_all_notifications(matches, file_path)
        
        # Step 4: Clean up
        print("Step 4: Cleaning up...")
        if success:
            status = "after successful notifications" if matches else "no faces matched"
            if self.delete_image(file_path):
                print(f"[DELETE] Reason: {status}")
        else:
            print(f"[KEEP] Retaining {get_file_info(file_path)}")
            print(f"[KEEP] Reason: One or more notifications failed to send")

    def on_created(self, event):
        """Handle file creation event"""
        if event.is_directory:
            return
            
        # If file doesn't have a valid extension, delete it
        if not event.src_path.lower().endswith(tuple(self.config.get_allowed_extensions())):
            try:
                file_info = get_file_info(event.src_path)
                os.remove(event.src_path)
                print(f"[DELETE] Removed non-image file: {file_info}")
                print(f"[DELETE] Reason: File extension not in allowed list {self.config.get_allowed_extensions()}")
            except OSError as e:
                print(f"[ERROR] Failed to delete file {event.src_path}: {e}")
            return
        
        # Process the file
        self.process_file(event.src_path)

def get_file_info(file_path: str) -> str:
    """Get formatted file info string including size and modification time"""
    try:
        stats = os.stat(file_path)
        size = humanize.naturalsize(stats.st_size)
        mtime = datetime.fromtimestamp(stats.st_mtime).strftime('%Y-%m-%d %H:%M:%S')
        return f"{os.path.basename(file_path)} ({size}, modified {mtime})"
    except OSError:
        return os.path.basename(file_path)

if __name__ == "__main__":
    main() 