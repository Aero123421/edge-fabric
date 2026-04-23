idf_component_register(
    SRCS "src/usb_link.c" "src/usb_tinyusb_backend.c"
    INCLUDE_DIRS "include"
    REQUIRES fabric_proto freertos
)
