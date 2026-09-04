package koba

import "context"

type descriptionFixtureRequest struct {
	Name string `json:"name"`
}

type descriptionFixtureResponse struct {
	ID int `json:"id"`
}

type descriptionFixtureAPI struct{}

func (*descriptionFixtureAPI) Create(_ context.Context, _ descriptionFixtureRequest) descriptionFixtureResponse {
	return descriptionFixtureResponse{}
}

func (*descriptionFixtureAPI) Routes() map[string]string {
	return map[string]string{"Create": "/v1/widgets"}
}

func (*descriptionFixtureAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{
		Name: "widgets",
		Operations: map[string]OperationDocumentation{
			"Create": {
				OperationID: "createWidget",
				Summary:     "Create a widget",
			},
		},
	}
}

type validationFixtureAPI struct {
	documentation APIDocumentation
}

type contractValidationFixtureAPI struct {
	documentation APIDocumentation
	contract      APIContract
}

func (contractValidationFixtureAPI) First()  {}
func (contractValidationFixtureAPI) Second() {}

func (api contractValidationFixtureAPI) APIDocumentation() APIDocumentation {
	return api.documentation
}

func (api contractValidationFixtureAPI) APIContract() APIContract {
	return api.contract
}

func (validationFixtureAPI) First() {}

func (validationFixtureAPI) Second() {}

func (api validationFixtureAPI) APIDocumentation() APIDocumentation {
	return api.documentation
}

type firstDefaultOperationAPI struct{}

func (firstDefaultOperationAPI) Get() {}

type secondDefaultOperationAPI struct{}

func (secondDefaultOperationAPI) Get() {}

type successStatusFixtureAPI struct {
	documentation OperationDocumentation
}

func (successStatusFixtureAPI) Get() descriptionFixtureResponse {
	return descriptionFixtureResponse{}
}

func (api successStatusFixtureAPI) APIDocumentation() APIDocumentation {
	return APIDocumentation{Operations: map[string]OperationDocumentation{"Get": api.documentation}}
}

type businessDocumentationMethodAPI struct{}

func (businessDocumentationMethodAPI) APIDocumentation() string { return "business response" }

type emptyRouteFixtureAPI struct{}

func (emptyRouteFixtureAPI) Get() {}

func (emptyRouteFixtureAPI) Routes() map[string]string {
	return map[string]string{"Get": ""}
}
