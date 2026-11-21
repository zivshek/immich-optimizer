// Decode Ultra HDR to upright, linear-sRGB float pixels, then encode or verify
// native HDR JXL. Validation uses libjxl directly rather than trusting tags.
#include <vips/vips.h>
#include <jxl/decode.h>
#include <algorithm>
#include <cmath>
#include <cstdio>
#include <fstream>
#include <iterator>
#include <limits>
#include <stdexcept>
#include <string>
#include <vector>

static void require(bool ok, const char *message) {
    if (!ok) throw std::runtime_error(message);
}

static void validate(VipsImage *reference, const char *filename) {
    std::ifstream file(filename, std::ios::binary);
    require(file.good(), "cannot open JXL output");
    std::vector<uint8_t> input((std::istreambuf_iterator<char>(file)), {});
    JxlDecoder *decoder = JxlDecoderCreate(nullptr);
    require(decoder != nullptr, "cannot create JXL decoder");
    struct DecoderCleanup { JxlDecoder *d; ~DecoderCleanup() { JxlDecoderDestroy(d); } } cleanup{decoder};
    require(JxlDecoderSetKeepOrientation(decoder, JXL_TRUE) == JXL_DEC_SUCCESS, "cannot preserve stored orientation");
    require(JxlDecoderSubscribeEvents(decoder, JXL_DEC_BASIC_INFO | JXL_DEC_COLOR_ENCODING | JXL_DEC_FULL_IMAGE) == JXL_DEC_SUCCESS, "cannot subscribe to decoder events");
    require(JxlDecoderSetInput(decoder, input.data(), input.size()) == JXL_DEC_SUCCESS, "cannot read JXL input");
    JxlDecoderCloseInput(decoder);
    JxlPixelFormat format{3, JXL_TYPE_FLOAT, JXL_NATIVE_ENDIAN, 0};
    std::vector<float> decoded;
    bool basic = false, color = false, frame = false;
    double intensity_target = 255.0; // libvips jxlsave default, unless supplied.
    if (vips_image_get_typeof(reference, "stonits")) {
        require(vips_image_get_double(reference, "stonits", &intensity_target) == 0, "cannot read HDR intensity target");
    }
    for (;;) {
        JxlDecoderStatus status = JxlDecoderProcessInput(decoder);
        if (status == JXL_DEC_BASIC_INFO) {
            JxlBasicInfo info;
            require(JxlDecoderGetBasicInfo(decoder, &info) == JXL_DEC_SUCCESS, "cannot read JXL header");
            require(info.xsize == static_cast<unsigned>(reference->Xsize) && info.ysize == static_cast<unsigned>(reference->Ysize), "HDR dimensions changed");
            require(info.num_color_channels == 3 && info.num_extra_channels == 0 && !info.have_animation, "unexpected JXL channels or animation");
            require(info.bits_per_sample == 32 && info.exponent_bits_per_sample == 8 && info.uses_original_profile, "HDR output must retain lossless 32-bit float samples");
            require(info.orientation == JXL_ORIENT_IDENTITY, "HDR output must have upright stored pixels");
            require(std::isfinite(info.intensity_target) && info.intensity_target > 0 && info.intensity_target == static_cast<float>(intensity_target), "HDR intensity signalling changed");
            basic = true;
        } else if (status == JXL_DEC_COLOR_ENCODING) {
            JxlColorEncoding encoding;
            require(JxlDecoderGetColorAsEncodedProfile(decoder, JXL_COLOR_PROFILE_TARGET_ORIGINAL, &encoding) == JXL_DEC_SUCCESS, "cannot verify JXL color encoding");
            require(encoding.color_space == JXL_COLOR_SPACE_RGB && encoding.white_point == JXL_WHITE_POINT_D65 && encoding.primaries == JXL_PRIMARIES_SRGB && encoding.transfer_function == JXL_TRANSFER_FUNCTION_LINEAR, "HDR color encoding must be linear sRGB/D65");
            color = true;
        } else if (status == JXL_DEC_NEED_IMAGE_OUT_BUFFER) {
            size_t size;
            require(JxlDecoderImageOutBufferSize(decoder, &format, &size) == JXL_DEC_SUCCESS, "cannot size JXL pixels");
            const size_t width = static_cast<size_t>(reference->Xsize);
            const size_t height = static_cast<size_t>(reference->Ysize);
            require(width <= std::numeric_limits<size_t>::max() / height / 3 / sizeof(float), "HDR image too large");
            require(size == width * height * 3 * sizeof(float), "unexpected decoded HDR pixel size");
            decoded.resize(size / sizeof(float));
            require(JxlDecoderSetImageOutBuffer(decoder, &format, decoded.data(), size) == JXL_DEC_SUCCESS, "cannot decode JXL pixels");
        } else if (status == JXL_DEC_FULL_IMAGE) {
            require(!frame, "unexpected extra HDR frame");
            frame = true;
        } else if (status == JXL_DEC_SUCCESS) {
            break;
        } else {
            throw std::runtime_error("JXL decoding failed or output is truncated");
        }
    }
    require(basic && color && frame && !decoded.empty(), "incomplete HDR JXL output");
    VipsRegion *region = vips_region_new(reference);
    require(region != nullptr, "cannot read reference HDR pixels");
    struct RegionCleanup { VipsRegion *r; ~RegionCleanup() { g_object_unref(r); } } region_cleanup{region};
    float maximum = -std::numeric_limits<float>::infinity();
    for (int y = 0; y < reference->Ysize; ++y) {
        VipsRect row{0, y, reference->Xsize, 1};
        require(vips_region_prepare(region, &row) == 0, "cannot reconstruct reference HDR pixels");
        const float *expected = reinterpret_cast<const float *>(VIPS_REGION_ADDR(region, 0, y));
        const size_t stride = static_cast<size_t>(reference->Xsize) * 3;
        for (size_t x = 0; x < stride; ++x) {
            float actual = decoded[static_cast<size_t>(y) * stride + x];
            require(std::isfinite(expected[x]) && std::isfinite(actual), "non-finite HDR samples");
            require(expected[x] == actual, "HDR pixel values changed (lossless validation failed)");
            maximum = std::max(maximum, actual);
        }
    }
    std::printf("[hdr-jxl] validated pixels=exact format=float32 color=linear-sRGB/D65 max=%g\n", maximum);
}

int main(int argc, char **argv) {
    if (argc != 4 || (std::string(argv[1]) != "encode" && std::string(argv[1]) != "validate")) {
        std::fprintf(stderr, "usage: iuo-hdr-jxl-codec encode|validate input.jpg output.jxl\n");
        return 2;
    }
    if (VIPS_INIT(argv[0])) return 1;
    VipsImage *source = nullptr, *hdr = nullptr, *upright = nullptr;
    int result = 0;
    try {
        source = vips_image_new_from_file(argv[2], nullptr);
        require(source != nullptr && vips_image_get_typeof(source, "gainmap-data"), "source has no decoded Ultra HDR gain map");
        require(vips_call("uhdr2scRGB", source, &hdr, nullptr) == 0, "Ultra HDR reconstruction failed");
        require(vips_autorot(hdr, &upright, nullptr) == 0, "cannot normalize HDR orientation");
        require(upright->Type == VIPS_INTERPRETATION_scRGB && upright->BandFmt == VIPS_FORMAT_FLOAT && upright->Bands == 3, "reference must be linear scRGB float32");
        // The source SDR ICC and gain-map XMP must not override native HDR.
        vips_image_remove(upright, VIPS_META_ICC_NAME);
        vips_image_remove(upright, VIPS_META_XMP_NAME);
        vips_image_remove(upright, VIPS_META_EXIF_NAME);
        if (std::string(argv[1]) == "encode") {
            require(vips_jxlsave(upright, argv[3], "distance", 0.0, "lossless", TRUE, "effort", 9, "keep", VIPS_FOREIGN_KEEP_NONE, nullptr) == 0, "native HDR JXL encoding failed");
        } else {
            validate(upright, argv[3]);
        }
    } catch (const std::exception &error) {
        std::fprintf(stderr, "[hdr-jxl] %s\n%s", error.what(), vips_error_buffer());
        result = 1;
    }
    VIPS_UNREF(upright);
    VIPS_UNREF(hdr);
    VIPS_UNREF(source);
    vips_shutdown();
    return result;
}
