class Parent {
  label() : String { "parent" };
};

class Child inherits Parent {
  label() : String { "child" };
  parent_label() : String { self@Parent.label() };
};

class Main inherits IO {
  main() : Object {
    let child : Child <- new Child in {
      out_string(child.label().concat("\n"));
      out_string(child.parent_label().concat("\n"));
    }
  };
};
