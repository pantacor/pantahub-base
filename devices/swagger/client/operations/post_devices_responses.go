package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"
	"io"

	"github.com/go-openapi/runtime"

	strfmt "github.com/go-openapi/strfmt"

	"pantahub-base/devices/swagger/models"
)

// PostDevicesReader is a Reader for the PostDevices structure.
type PostDevicesReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the recieved o.
func (o *PostDevicesReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {

	case 200:
		result := NewPostDevicesOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil

	default:
		result := NewPostDevicesDefault(response.Code())
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	}
}

// NewPostDevicesOK creates a PostDevicesOK with default headers values
func NewPostDevicesOK() *PostDevicesOK {
	return &PostDevicesOK{}
}

/*PostDevicesOK handles this case with default header values.

Echos the newly created device resource back to the client with id
updated to the actual id.
Also sets Location header to the URL of the created Device.

*/
type PostDevicesOK struct {
	Payload *models.Device
}

func (o *PostDevicesOK) Error() string {
	return fmt.Sprintf("[POST /devices/][%d] postDevicesOK  %+v", 200, o.Payload)
}

func (o *PostDevicesOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Device)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewPostDevicesDefault creates a PostDevicesDefault with default headers values
func NewPostDevicesDefault(code int) *PostDevicesDefault {
	return &PostDevicesDefault{
		_statusCode: code,
	}
}

/*PostDevicesDefault handles this case with default header values.

Unexpected Error
*/
type PostDevicesDefault struct {
	_statusCode int

	Payload *models.Error
}

// Code gets the status code for the post devices default response
func (o *PostDevicesDefault) Code() int {
	return o._statusCode
}

func (o *PostDevicesDefault) Error() string {
	return fmt.Sprintf("[POST /devices/][%d] PostDevices default  %+v", o._statusCode, o.Payload)
}

func (o *PostDevicesDefault) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
